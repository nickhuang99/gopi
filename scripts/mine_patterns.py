#!/usr/bin/env python3
"""Offline pattern miner — agency-proposals epic evidence pass (read-only).

Mines ~/.gopi (tasks ledger + sessions JSONL + knowledge base) for
candidate recurring patterns. Prints a compact JSON evidence block.
No writes anywhere: the whole pass is read-only (sqlite mode=ro).
"""
import sqlite3, re, json, os, sys, glob, collections

HOME = os.path.expanduser("~")
DB = os.path.join(HOME, ".gopi", "memory.db")
SESS = os.path.join(HOME, ".gopi", "sessions")

def norm(s):
    s = (s or "").lower()
    s = re.sub(r"\b[0-9a-f]{7,}\b", "<h>", s)
    s = re.sub(r"\d+", "<n>", s)
    s = re.sub(r"[^a-z0-9\u4e00-\u9fff<>]+", " ", s)
    return " ".join(s.split())

def tasks():
    db = sqlite3.connect("file:%s?mode=ro" % DB, uri=True)
    return db

out = {}

# ---------- 1. task summary signature clusters ----------
db = tasks()
rows = db.execute("select task_id,summary,tags,source,status,created_at,tasklist_issued_at from tasks").fetchall()
struct = collections.Counter()
for r in rows:
    n = norm(r[1])
    toks = n.replace("[", " ").replace("]", " ").split()
    key = " ".join(toks[:3])
    struct[key] += 1
out["task_clusters_ge4"] = [{"key": k, "n": v} for k, v in struct.most_common(30) if v >= 4]

# ---------- 2. task_events: flapping / reopen / repeated failure ----------
ev = db.execute("select task_id,event,count(*) from task_events group by task_id,event").fetchall()
per_task = collections.defaultdict(dict)
for tid, e, n in ev:
    per_task[tid][e] = n
multi_reopen = [t for t, d in per_task.items() if d.get("reopened", 0) >= 2]
out["tasks_reopened_ge2"] = len(multi_reopen)
# branch-work loops (merge conflict tasks per branch)
brow = db.execute("select task_id,summary,tags,status from tasks where tags like '%merge-conflict%'").fetchall()
br = collections.Counter()
for tid, s, tg, st in brow:
    m = re.search(r"(?:conflict:|merge-backlog:)\s*([^\s—]+)", s)
    br[m.group(1) if m else norm(s)[:40]] += 1
out["merge_conflict_clusters_ge2"] = [{"branch": k, "n": v} for k, v in br.most_common(20) if v >= 2]

# ---------- 3. failed tasks: repeated failure signatures ----------
fail = db.execute("select summary,tags from tasks where status='failed'").fetchall()
fc = collections.Counter(" ".join(norm(s).split()[:3]) for s, _ in fail)
out["failed_task_clusters_ge3"] = [{"key": k, "n": v} for k, v in fc.most_common(12) if v >= 3]
out["failed_total"] = len(fail)

# ---------- 4. knowledge base health ----------
qa = db.execute("select topic,question,retrieval_count,useful_count,useless_count,tier from distilled_qa").fetchall()
out["qa_total"] = len(qa)
out["qa_zero_retrieval"] = sum(1 for r in qa if not r[2])
out["qa_useless_ge1"] = sum(1 for r in qa if (r[4] or 0) >= 1)
qs = collections.Counter(norm(r[1])[:60] for r in qa)
out["qa_dup_clusters_ge3"] = [{"key": k, "n": v} for k, v in qs.most_common(20) if v >= 3]

# ---------- 5. sessions: tool-call sequences + repeated failures ----------
seqs = collections.Counter()
bashcmd = collections.Counter()
failsig = collections.Counter()
nsess = 0
files = glob.glob(os.path.join(SESS, "*", "*.jsonl"))
for f in files:
    if f.endswith("session_parts.jsonl"):
        continue
    try:
        if os.path.getsize(f) < 200:
            continue
    except OSError:
        continue
    nsess += 1
    seq = []
    for line in open(f, "r", errors="replace"):
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        t = d.get("type")
        if t == "assistant":
            msg = d.get("message") or {}
            for blk in (msg.get("content") or []):
                if isinstance(blk, dict) and blk.get("type") == "tool_use":
                    name = blk.get("name") or blk.get("Name") or "?"
                    seq.append(name)
                    if name == "bash":
                        inp = blk.get("input") or {}
                        cmd = inp.get("command") or ""
                        head = " ".join(cmd.split()[:3])
                        bashcmd[head] += 1
        elif t == "tool_result" or d.get("role") == "user":
            pass
    if seq:
        seqs[" ".join(seq[:6])] += 1
out["sessions_with_jsonl"] = nsess
out["tool_prefix_seqs_ge5"] = [{"seq": k, "n": v} for k, v in seqs.most_common(15) if v >= 5]
out["bash_head_ge10"] = [{"cmd": k, "n": v} for k, v in bashcmd.most_common(40) if v >= 10]

print(json.dumps(out, ensure_ascii=False, indent=1))
