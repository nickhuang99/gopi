#!/usr/bin/env python3
# test-drivable: no one-shot lifecycle analysis script reading fixed argv positions at module scope
# NOTE: reads sys.argv at module scope (one-shot analysis script).
# To make it test-drivable, thread argv through a main(argv=None).
"""H1-H5 hypothesis tests over extract2.jsonl (see lifecycle-chain-audit.py).

Reads:  the extract2.jsonl produced by lifecycle-chain-audit.py extract
        (per-tool_use records + tool_result heads), path passed as argv[1].
Writes: nothing (stdout only) — all figures quoted in the report come from here.

Eras (decided by the tool-surface merge of 2026-09-10, which renamed the wire
names; see NAME_MAP in lifecycle-chain-audit.py):
  E1  <= 2026-08-27   flat tool names, pre tool-persuasion work
  E2  2026-08-28..09-09  code_search/git_blame routing push + memsync era
  E3  2026-09-10.. >=    merged memory/task/context_trim surface
"""
import json
import re
import sys
from collections import Counter, defaultdict

if len(sys.argv) < 2:
    sys.exit("usage: lifecycle-hypotheses.py <extract2.jsonl>")
src = sys.argv[1]
MODULE_BLOCK = range(900, 1010)  # ids of the 2026-08-27 code-reading block


def era(ts):
    if not ts:
        return "?"
    d = ts[:10]
    if d <= "2026-08-27":
        return "E1"
    if d <= "2026-09-09":
        return "E2"
    return "E3"


def main():
    recs = [json.loads(l) for l in open(src) if l.strip()]
    calls = Counter()
    by_era = defaultdict(Counter)
    sess_era = {}
    sess_tools = defaultdict(set)
    search_hits = Counter()          # distilled_qa id -> times returned
    search_hits_by_era = defaultdict(lambda: Counter())
    searched_ids_total = 0
    search_result_q = []
    understand_by_era = defaultdict(lambda: Counter())
    store_by_era = Counter()
    research_by_era = Counter()
    sessions_by_era = defaultdict(set)

    for r in recs:
        e = era(r["ts"])
        t = r["tool"]
        calls[t] += 1
        by_era[e][t] += 1
        sid = r["session_id"]
        sess_era[sid] = e
        sess_tools[sid].add(t)
        sessions_by_era[e].add(sid)
        if t in ("read", "code_search", "keyword_search", "git_blame"):
            understand_by_era[e][t] += 1
        if t in ("web_search", "web_fetch"):
            research_by_era[e] += 1
        if t in ("memory:store", "memory:update"):
            store_by_era[e] += 1
        if t == "memory:search":
            ids = [int(x) for x in re.findall(r"#(\d+).{0,300}?tier=", r.get("result_head", ""))]
            for i in ids:
                search_hits[i] += 1
                search_hits_by_era[e][i] += 1
            searched_ids_total += len(ids)
            search_result_q.append((r["ts"], sid, str(r["input"].get("query"))[:80], ids[:8]))

    print("== tool census by era ==")
    tools = sorted({t for c in by_era.values() for t in c})
    print("%-16s %7s %7s %7s" % ("tool", "E1", "E2", "E3"))
    for t in tools:
        print("%-16s %7d %7d %7d" % (t, by_era["E1"][t], by_era["E2"][t], by_era["E3"][t]))
    print("%-16s %7d %7d %7d" % ("sessions", len(sessions_by_era["E1"]),
                                 len(sessions_by_era["E2"]), len(sessions_by_era["E3"])))

    print("\n== per-era ratios ==")
    for e in ("E1", "E2", "E3"):
        r_ = understand_by_era[e]["read"]
        c_ = understand_by_era[e]["code_search"]
        g_ = understand_by_era[e]["git_blame"]
        w_ = research_by_era[e]
        st = store_by_era[e]
        s = len(sessions_by_era[e]) or 1
        print("  %s: read=%d code_search=%d ratio=%.1f  git_blame=%d  web=%d(%.2f/sess) mem_store=%d(%.2f/sess)"
              % (e, r_, c_, (r_ / c_ if c_ else float("inf")), g_, w_, w_ / s, st, st / s))

    print("\n== H1/H2 kill-shot: does the code-reading corpus ever come back in a search? ==")
    blk = {i: search_hits[i] for i in sorted(search_hits) if i in MODULE_BLOCK}
    print("  distilled_qa rows ever returned by a memory search: %d of 1808" % len(search_hits))
    print("  rows in the 2026-08-27 module block (900-1009) ever returned: %d of %d"
          % (len(blk), len(MODULE_BLOCK)))
    print("  those rows and their hit counts:", blk)
    print("  total hit-instances   :", sum(search_hits.values()))
    print("  top-15 most returned  :", search_hits.most_common(15))
    print("  most-returned ids inside the module block:",
          Counter({k: v for k, v in search_hits.items() if k in MODULE_BLOCK}).most_common(5))

    print("\n== H1: repeated web research (never re-uses a stored answer) ==")
    for ts, sid, q, ids in search_result_q[:0]:
        pass
    print("  see research-cluster block in the report (Jaccard script)")

    print("\n== per-session sink coverage by era ==")
    sink_eras = defaultdict(lambda: [0, 0])
    for sid, tools in sess_tools.items():
        e = sess_era[sid]
        sink_eras[e][0] += 1
        if tools & {"memory:store", "memory:update"}:
            sink_eras[e][1] += 1
    for e in ("E1", "E2", "E3"):
        n, k = sink_eras[e]
        print("  %s: %d/%d sessions (%d%%) called memory:store|update"
              % (e, k, n, round(100 * k / max(n, 1))))


main()
