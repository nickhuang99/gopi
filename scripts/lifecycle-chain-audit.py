#!/usr/bin/env python3
"""Lifecycle chain evidence collector (READ-ONLY).

Extracts, from ~/.gopi/sessions/**/*.jsonl (what actually happened) and
~/.gopi/memory.db (what was left behind), the aggregates used by
devdoc/lifecycle-chain-evidence-20260911.md.

Usage:
    python3 scripts/lifecycle-chain-audit.py actions   ~/.gopi/sessions  > actions.json
    python3 scripts/lifecycle-chain-audit.py sessions  ~/.gopi/sessions  > sessions.json
    python3 scripts/lifecycle-chain-audit.py research  actions.json      > research.json
    python3 scripts/lifecycle-chain-audit.py tree        ~/.gopi/sessions  > tree.json

Never writes to the sessions tree or to memory.db.
"""
import json
import os
import re
import sys
from collections import Counter, defaultdict

# ---- tool-name normalisation -------------------------------------------------
# The session JSONL spans the 2026-09-10 tool-surface merge: old wire names
# (memory_search / todo / write_task_area / task_evaluate) must fold onto the
# current (memory / task) names for a cross-era count to mean anything.
NAME_MAP = {
    "memory_search": "memory:search",
    "memory_store": "memory:store",
    "memory_update": "memory:update",
    "memory_feedback": "memory:feedback",
    "write_task_area": "task:commit",
    "todo": "task:view",
    "task_evaluate": "task:reframe",
}


def norm(name: str, inp: dict | None) -> str:
    if name in ("memory", "task") and isinstance(inp, dict):
        act = inp.get("action")
        if act:
            return f"{name}:{act}"
    return NAME_MAP.get(name, name)


TOOL_CATEGORY = {
    "research": {"web_search", "web_fetch", "memory:search", "code_search", "keyword_search"},
    "understand": {"read", "code_search", "keyword_search", "git_blame"},
    "debug": {"bash"},
    "sink_memory": {"memory:store", "memory:update"},
    "sink_task": {"task:commit", "task_submit"},
    "feedback": {"memory:feedback"},
}

# bash commands that are explicitly informational (read-only inspection) —
# a crude probe is better than none; the report states the method and its limits.
INFO_CMD = re.compile(
    r"\b(git\s+(log|blame|show|diff|status|branch|worktree)|grep|rg|find|ls|wc|"
    r"cat|head|tail|ps|which|sqlite3|\.schema|sha256sum|date)\b"
)
MUTATING_CMD = re.compile(
    r"\b(go\s+(build|test|vet)|make|git\s+(add|commit|checkout|merge|push|fetch)|"
    r"rm|mv|cp|mkdir|touch|sed\s+-i|python3|curl|npm|pip|sqlite3\s+\S+\s+\"?(INSERT|UPDATE|DELETE))\b",
    re.I,
)


def iter_events(root):
    """Yield (path, line_no, obj) for every JSONL line under root."""
    for dirpath, _dirnames, filenames in os.walk(root):
        for fn in filenames:
            if not fn.endswith(".jsonl"):
                continue
            p = os.path.join(dirpath, fn)
            try:
                with open(p, encoding="utf-8", errors="replace") as fh:
                    for i, line in enumerate(fh):
                        line = line.strip()
                        if not line:
                            continue
                        try:
                            yield p, i, json.loads(line)
                        except Exception:
                            continue
            except OSError:
                continue


def cmd_kind(cmd: str) -> str:
    if MUTATING_CMD.search(cmd):
        return "mutating"
    if INFO_CMD.search(cmd):
        return "informational"
    return "other"


def action(kind, root, out):
    """Per-session tool_use census."""
    per_session = {}
    for p, ln, o in iter_events(root):
        if o.get("type") != "assistant":
            continue
        session = o.get("sessionId") or os.path.basename(os.path.dirname(p))
        rec = per_session.setdefault(
            session,
            {"session_id": session, "path": p, "tool_calls": 0, "by_name": Counter(),
             "bash_kind": Counter(), "ts_first": None, "ts_last": None, "task_id": o.get("taskId", "")},
        )
        ts = o.get("timestamp")
        if ts:
            rec["ts_first"] = rec["ts_first"] or ts
            rec["ts_last"] = ts
        for c in o.get("message", {}).get("content", []) or []:
            if not isinstance(c, dict) or c.get("type") != "tool_use":
                continue
            nm = norm(c.get("name", "?"), c.get("input"))
            rec["tool_calls"] += 1
            rec["by_name"][nm] += 1
            if nm == "bash":
                rec["bash_kind"][cmd_kind(str(c.get("input", {}).get("command", "")))] += 1
    json.dump(
        [dict(v, by_name=dict(v["by_name"]), bash_kind=dict(v["bash_kind"]))
         for v in per_session.values()],
        open(out, "w"), ensure_ascii=False, indent=0,
    )
    print(f"{len(per_session)} sessions -> {out}", file=sys.stderr)


def research(actions_json):
    """Research/understand/debug volume vs memory sink volume, per session."""
    rows = json.load(open(actions_json))
    tot = Counter()
    sessions_with_research = 0
    sessions_with_research_and_store = 0
    sessions_with_understand = 0
    sessions_with_understand_and_store = 0
    sessions_with_debug = 0
    sessions_with_debug_and_store = 0
    detail = []
    for r in rows:
        names = r["by_name"]
        res = sum(names.get(k, 0) for k in TOOL_CATEGORY["research"])
        und = sum(names.get(k, 0) for k in TOOL_CATEGORY["understand"])
        dbg = r["bash_kind"].get("mutating", 0)
        store = sum(names.get(k, 0) for k in TOOL_CATEGORY["sink_memory"])
        for k, v in names.items():
            tot[k] += v
        if res:
            sessions_with_research += 1
            if store:
                sessions_with_research_and_store += 1
        if und:
            sessions_with_understand += 1
            if store:
                sessions_with_understand_and_store += 1
        if dbg:
            sessions_with_debug += 1
            if store:
                sessions_with_debug_and_store += 1
        detail.append({"session_id": r["session_id"], "tasks": r["task_id"],
                       "research": res, "understand": und, "mutating_bash": dbg,
                       "mem_store": names.get("memory:store", 0),
                       "mem_search": names.get("memory:search", 0),
                       "task_commit": names.get("task:commit", 0),
                       "tool_calls": r["tool_calls"], "ts_first": r["ts_first"], "ts_last": r["ts_last"]})
    json.dump({"totals": dict(tot), "sessions": len(rows),
               "with_research": sessions_with_research,
               "with_research_and_store": sessions_with_research_and_store,
               "with_understand": sessions_with_understand,
               "with_understand_and_store": sessions_with_understand_and_store,
               "with_debug": sessions_with_debug,
               "with_debug_and_store": sessions_with_debug_and_store,
               "detail": detail}, open("/dev/stdout", "w"), ensure_ascii=False, indent=0)


FLAG_RESULT = re.compile(
    r"(FAIL|panic:|exit status [1-9]|SIGSEGV|error:|Error:|undefined:|cannot use|"
    r"no such file|traceback|BUILD FAILED|does not compile)", re.I)


def extract(root, out):
    """One JSONL record per interesting tool_use, paired with its tool_result.

    The pairing key is the call id: tool_result rows carry
    '[tool_use id: call_...]' in their text (gopi session format).
    """
    results = {}
    events = []
    for p, ln, o in iter_events(root):
        t = o.get("type")
        if t == "tool_result":
            txt = json.dumps(o.get("message", {}), ensure_ascii=False)
            m = re.search(r"tool_use id: (call_[A-Za-z0-9_]+)", txt)
            if m:
                results.setdefault(m.group(1), txt)
        elif t == "assistant":
            sid = o.get("sessionId") or os.path.basename(os.path.dirname(p))
            for c in o.get("message", {}).get("content", []) or []:
                if isinstance(c, dict) and c.get("type") == "tool_use":
                    events.append((sid, o.get("timestamp"), c, p))
    keep = {"memory:store", "memory:update", "memory:search", "memory:feedback",
            "web_search", "web_fetch", "code_search", "task_submit", "task:commit",
            "read", "git_blame"}
    n = 0
    with open(out, "w", encoding="utf-8") as fh:
        for sid, ts, c, p in events:
            nm = norm(c.get("name", "?"), c.get("input"))
            if nm not in keep and nm != "bash":
                continue
            inp = c.get("input", {}) if isinstance(c.get("input"), dict) else {}
            res = results.get(c.get("id", ""), "")
            rec = {"session_id": sid, "ts": ts, "tool": nm, "path": p,
                   "input": inp, "result_len": len(res)}
            if nm in ("memory:search", "read", "code_search", "web_search", "git_blame"):
                rec["result_head"] = res[:2000]
                fh.write(json.dumps(rec, ensure_ascii=False) + "\n")
                n += 1
                continue
            if nm == "bash":
                cmd = str(inp.get("command", ""))
                k = cmd_kind(cmd)
                if k == "mutating":
                    rec["kind"] = "mutating"
                    rec["cmd"] = cmd[:400]
                    rec["result_flag"] = bool(FLAG_RESULT.search(res))
                    rec["result_head"] = res[:600]
                    fh.write(json.dumps(rec, ensure_ascii=False) + "\n")
                    n += 1
                continue
            fh.write(json.dumps(rec, ensure_ascii=False) + "\n")
            n += 1
    print(f"{n} records -> {out}", file=sys.stderr)


def tree(root):
    """Sessions tree shape: per-day session counts (sampling frame evidence)."""
    days = Counter()
    for dirpath, _d, filenames in os.walk(root):
        for fn in filenames:
            if not fn.endswith(".jsonl"):
                continue
            p = os.path.join(dirpath, fn)
            try:
                mt = os.path.getmtime(p)
            except OSError:
                continue
            import datetime
            days[datetime.datetime.fromtimestamp(mt).strftime("%Y-%m-%d")] += 1
    json.dump(dict(sorted(days.items())), open("/dev/stdout", "w"), indent=1)


if __name__ == "__main__":
    argv = sys.argv[1:] if argv is None else list(argv)
    what = argv[0]
    if what == "actions":
        action(what, argv[1], argv[2])
    elif what == "extract":
        extract(argv[1], argv[2])
    elif what == "research":
        research(argv[1])
    elif what == "tree":
        tree(argv[1])
    else:
        sys.exit(f"unknown mode {what}")
