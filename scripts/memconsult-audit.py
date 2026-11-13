#!/usr/bin/env python3
"""memconsult-audit.py — bounded historical audit: the 忘記先查 case list (D3).

Answers, from data rather than taste: in the recent session window, how often
did a run start EXPLORING before it ever consulted memory, what did that cost
(tool calls + tokens + seconds before the first memory search), and was there
anything to find (a distilled row that existed BEFORE the session started)?

The two case classes the epic demands be kept apart:

  真丢      (truly lost)        — the run explored first and NO applicable row
                                  existed yet: the knowledge was never stored.
  存了没想起 (stored-unconsulted) — an applicable row DID exist (created before
                                  the session, never retrieved since), and the
                                  run made progress without it. This is the
                                  class the epic's "Bond re-explores our own
                                  memory-sync code" case belongs to.

It also classifies the 忘記先查 population proper — runs where the harness DID
ask the start-of-run question (its marker is in the log) and the run never
consulted memory: `never-consulted-had-row` (a distilled row was waiting =
存了但没想起来查) vs `never-consulted-nothing-stored` (真丢). Both carry the
run's token cost.

It also measures the candidate-B firing signature (D4 input): a run that spends
>= STUCK_EXPLORE_ROUNDS consecutive exploration rounds in the same area with no
memory search in between — how many such runs exist, and in how many of them a
pre-existing applicable row was waiting (the would-have-been-useful rate).

Bounded by construction: only the newest --sessions session directories are
opened, and each JSONL is streamed a single pass, line by line. The live WAL
memory.db is opened READ-ONLY (file:...?mode=ro) — never copied (memory
#1614/#1862). Stdlib only.

Usage:
  python3 scripts/memconsult-audit.py --sessions 300 --out "$SCRATCH/memconsult-audit.json"
"""

import argparse
import glob
import json
import os
import re
import sqlite3
import sys
import time

# ── bounded scan (Rule 1: every bound is a named constant) ─────────────────
DEFAULT_SESSIONS = 300          # newest N session logs (epic constraint 11)
DEFAULT_WINDOW_DAYS = 14        # only logs touched within this many days
SECONDS_PER_DAY = 86400
MIN_RUN_TOOL_CALLS = 1          # below this a session log is not a work run
MAX_LINE_BYTES = 8 * 1024 * 1024
TOKEN_MIN_LEN = 4               # latin token minimum length
CJK_MIN_RUN = 2                 # a CJK run this long becomes a subject token
SUBJECT_OVERLAP_MIN = 2         # overlapping subject tokens ⇒ "applicable row"

# ── candidate-B firing signature ──────────────────────────────────────────
STUCK_EXPLORE_ROUNDS = 3        # consecutive same-area exploration rounds
AREA_OVERLAP_MIN = 2            # area-token overlap ⇒ a row covers that area

# ── tool-name classes (wire names; Rule 1b keeps them in one place) ────────
# The memory family was MERGED (memory search/store/update/feedback → one
# `memory` tool with an action field). Historical logs therefore carry TWO
# wire spellings of the same act, and a detector that knows only the current
# one reports every pre-merge run as "never consulted memory" — the exact
# measurement defect that would have turned into "341 runs wasted 5.1B tokens"
# instead of "the detector was blind to half its window". Both spellings are
# constants here, and both are matched.
TOOL_MEMORY = "memory"
TOOL_MEMORY_SEARCH_LEGACY = "memory_search"
TOOL_MEMORY_FEEDBACK_LEGACY = "memory_feedback"
TOOL_MEMORY_UPDATE_LEGACY = "memory_update"
TOOL_READ = "read"
TOOL_BASH = "bash"
TOOL_CODE_SEARCH = "code_search"
TOOL_KEYWORD_SEARCH = "keyword_search"
TOOL_WRITE = "write"
TOOL_EDIT = "edit"
TOOL_GIT_BLAME = "git_blame"
EXPLORE_TOOLS = {TOOL_READ, TOOL_BASH, TOOL_CODE_SEARCH, TOOL_KEYWORD_SEARCH,
                 TOOL_GIT_BLAME}
MEMORY_ACTION_SEARCH = "search"
MEMORY_HIT_LINE_PREFIX = "#"   # trajectory.go parseHitRowIDs: a hit line is "#<id> …"
TASK_START_MARKER = "TASK-START CHECKPOINT"   # loop.go taskStartSearchMessage
TASK_START_PREFIX = "[system] TASK-START CHECKPOINT"   # the injection's first chars
CONSULT_MARKER = "MEMORY-CONSULT CHECKPOINT"  # loop.go consultCheckpointMarker


def is_injection(text, marker):
    """True when a user-message text IS the harness injection, not merely a text
    that CONTAINS the marker.

    The injections are prepended to a fresh user message, so they are at the
    START of it (`[system] TASK-START CHECKPOINT…`, `[MEMORY-CONSULT
    CHECKPOINT]…`). A substring test is NOT enough, and the evidence is
    unambiguous: this work's own report text quotes both markers, and the
    harness echoes the work area back as a user message, so a substring count
    reported **42 consult-checkpoint events over 37 runs** — while the
    mechanism is one-shot by construction. Counted by injection shape the
    events collapse to 37, exactly one per run: the 5 phantom events were the
    reader counting its own words. Seventh measurement defect found this way
    (the instruments, never the mechanisms)."""
    return text.lstrip().startswith(marker)

STOPWORDS = {
    "that", "this", "with", "from", "have", "been", "were", "they", "them",
    "then", "than", "when", "which", "what", "your", "into", "over", "only",
    "must", "should", "will", "would", "could", "there", "their", "about",
    "after", "before", "these", "those", "each", "some", "such", "also",
    "using", "used", "use", "file", "files", "line", "lines", "code", "test",
    "tests", "true", "false", "func", "return", "string", "error", "task",
    "tasks", "run", "runs", "gopi", "https", "http", "the", "and", "for",
}

SESSION_FILE_GLOB = os.path.expanduser("~/.gopi/sessions/*/*.jsonl")
MEMORY_DB = os.path.expanduser("~/.gopi/memory.db")


def tokenize(text):
    """Subject tokens: latin words (len>=TOKEN_MIN_LEN, non-stopword) plus CJK
    runs of length >= CJK_MIN_RUN. One tokenizer for BOTH sides of the overlap
    test, so the comparison is apples to apples."""
    if not text:
        return set()
    out = set()
    for tok in re.findall(r"[A-Za-z0-9_./-]+", text.lower()):
        for part in re.split(r"[^a-z0-9]+", tok):
            if len(part) >= TOKEN_MIN_LEN and part not in STOPWORDS:
                out.add(part)
    for run in re.findall(r"[\u4e00-\u9fff]{%d,}" % CJK_MIN_RUN, text):
        out.add(run)
    return out


def parse_rfc3339(text):
    """The session log's timestamp field → epoch seconds (0.0 when unparsable;
    the classifier treats 0 as 'before everything', never as 'now')."""
    if not text:
        return 0.0
    try:
        from datetime import datetime, timezone
        return datetime.strptime(text[:19], "%Y-%m-%dT%H:%M:%S").replace(
            tzinfo=timezone.utc).timestamp()
    except ValueError:
        return 0.0


def load_memory_rows(db_path):
    """Every distilled row, READ-ONLY. Returns a list of dicts with the fields
    the audit needs to decide 'was there something to find'."""
    con = sqlite3.connect("file:%s?mode=ro" % db_path, uri=True)
    con.row_factory = sqlite3.Row
    try:
        rows = con.execute(
            "select id, topic, question, answer, search_kw, sources, tier, "
            "distilled_at, retrieval_count, last_retrieved from distilled_qa"
        ).fetchall()
    finally:
        con.close()
    out = []
    for r in rows:
        subject = " ".join(filter(None, [r["topic"], r["search_kw"],
                                         r["sources"], r["question"]]))
        out.append({
            "id": r["id"],
            "tier": r["tier"],
            "label": (r["topic"] or "")[:110],
            "subject": tokenize(subject),
            "sources": r["sources"] or "",
            "distilled_at": r["distilled_at"] or 0.0,
            "retrieval_count": r["retrieval_count"] or 0,
            "last_retrieved": r["last_retrieved"] or 0.0,
        })
    return out


def newest_session_files(root_glob, limit, window_days=DEFAULT_WINDOW_DAYS,
                         now=None):
    """The newest `limit` session JSONL paths whose mtime is inside
    `window_days`. The window is what makes the scan RECENT (a plain "newest
    N" bound is not enough: the sessions root holds 24k directories, of which
    the newest by directory mtime can still be months old — the dir's mtime
    does not move when a nested log is appended). Both bounds are applied
    BEFORE any file is opened (epic constraint 11)."""
    cutoff = (now if now is not None else time.time()) - \
        window_days * SECONDS_PER_DAY
    files = glob.glob(root_glob)
    files = [p for p in files if os.path.getmtime(p) >= cutoff]
    files.sort(key=os.path.getmtime, reverse=True)
    return files[:limit]


def is_memory_search(name, inp):
    """The memory-search act, in BOTH wire spellings the log window carries:
    the merged `memory` tool with action=search, and the pre-merge
    `memory_search` tool. One predicate so the caller cannot match one and
    forget the other (that omission is what produced the false 忘記先查 flood)."""
    if name == TOOL_MEMORY_SEARCH_LEGACY:
        return True
    return (name == TOOL_MEMORY and isinstance(inp, dict)
            and inp.get("action") == MEMORY_ACTION_SEARCH)


def is_memory_consume(name, inp):
    """The memory-adoption act (feedback/update naming a row id), both
    spellings.""" 
    if name in (TOOL_MEMORY_FEEDBACK_LEGACY, TOOL_MEMORY_UPDATE_LEGACY):
        return True
    return (name == TOOL_MEMORY and isinstance(inp, dict)
            and inp.get("action") in ("feedback", "update"))


def tool_result_blocks(ev):
    """The tool_result blocks of a `tool_result`-type event (the wire shape the
    session log writes: message.content is a list of blocks carrying
    tool_use_id + content). A `user`-type event carries string content and is
    not a tool result — yield nothing for it."""
    if ev.get("type") != "tool_result":
        return []
    content = (ev.get("message") or {}).get("content")
    if not isinstance(content, list):
        return []
    return [b for b in content
            if isinstance(b, dict) and b.get("type") == "tool_result"]


def row_ids_in_result(content):
    """The distilled row ids a memory-search result handed back, counted the way
    trajectory.go's parseHitRowIDs counts them (a line whose first non-blank
    character is '#<digits>') — ONE definition of "a hit line" for both the Go
    telemetry and this audit, so the two instruments cannot disagree about what
    an EMPTY search is.

    The result content field is a plain string in the wire shape gopi writes,
    but the Anthropic schema allows a block array; both are handled, and an
    unrecognised shape yields None (UNKNOWN — never silently "empty", which
    would inflate the zero-hit population with the reader's own blindness)."""
    if isinstance(content, str):
        text = content
    elif isinstance(content, list):
        text = "".join(b.get("text", "") for b in content
                       if isinstance(b, dict))
    else:
        return None
    ids = []
    for line in text.split("\n"):
        stripped = line.lstrip()
        if not stripped.startswith(MEMORY_HIT_LINE_PREFIX):
            continue
        end = 1
        while end < len(stripped) and stripped[end].isdigit():
            end += 1
        if end > 1:
            ids.append(stripped[1:end])
    return ids


def tool_input_area(name, inp):
    """The AREA a tool call addressed — the key behind the déjà-vu signature.
    A read/edit/write names a file; a search names its query/pattern; bash
    names its command's leading token."""
    if not isinstance(inp, dict):
        return ""
    for key in ("file_path", "path", "notebook_path"):
        if inp.get(key):
            return os.path.basename(str(inp[key]))
    for key in ("pattern", "query", "symbol"):
        if inp.get(key):
            return str(inp[key])[:80]
    if name == TOOL_BASH and inp.get("command"):
        return str(inp["command"])[:80]
    return ""


def scan_session(path, rows, stuck_rounds):
    """One streaming pass over one session JSONL. Returns the per-session
    audit record (or None when the log carries no usable start event)."""
    rec = {
        "session": os.path.basename(path).rsplit(".", 1)[0],
        "task_id": "", "workdir": "", "model": "",
        "start_ts": 0.0,
        "rounds": 0,
        "first_tool_round": -1,
        "first_search_round": -1,
        "tool_calls_before_search": 0,
        "completion_tokens_before_search": 0,
        "prompt_tokens_before_search": 0,
        "tool_calls_total": 0,
        "searches": 0,
        "first_search_id": "",
        "search_result_rows": {},
        "search_subject": set(),
        "areas": [],
        "search_rounds": [],
        "stuck_explore_round": -1,
        "stuck_area": "",
        "round_completion": [],
        "completion_tokens_after_stuck": 0,
        "rounds_after_stuck": 0,
        "explored_first": False,
        "start_checkpoint": False,
        "consult_checkpoints": 0,
        "ended": False,
        "total_tokens": 0,
        "elapsed_sec": 0.0,
        "exit_code": 0,
    }
    round_idx = 0
    areas_by_round = {}
    last_search_round = -1
    round_started = False
    first_user_text = ""
    # Search tool_use ids this run issued, and the row ids their results handed
    # back. Populated in ONE place each, so the zero-hit population (the
    # candidate-B replacement hypothesis) is measured rather than assumed.
    search_ids = set()
    search_rows = {}
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            if not line.strip():
                continue
            try:
                ev = json.loads(line[:MAX_LINE_BYTES])
            except (ValueError, TypeError):
                continue
            etype = ev.get("type")
            if etype == "session_start":
                rec["task_id"] = ev.get("taskId", "")
                rec["workdir"] = ev.get("workdir", "")
                rec["model"] = ev.get("model", "")
                rec["start_ts"] = parse_rfc3339(ev.get("timestamp", ""))
                continue
            if etype == "user":
                msg = ev.get("message") or {}
                content = msg.get("content")
                if isinstance(content, str):
                    texts = [content]
                elif isinstance(content, list):
                    texts = [str(b.get("content", "")) for b in content
                             if isinstance(b, dict)]
                else:
                    texts = []
                for t in texts:
                    # Both harness injections are logged as user messages.
                    # The task-start one is usually the FIRST user message of a
                    # non-epic run, so the marker must be checked on every user
                    # message (an `elif` after "the first one is the briefing"
                    # silently misses exactly the runs that got the injection).
                    # The match is on the injection SHAPE (the marker at the
                    # start), never a bare substring — see is_injection.
                    if is_injection(t, TASK_START_PREFIX):
                        rec["start_checkpoint"] = True
                        continue
                    if CONSULT_MARKER in t and is_injection(
                            t, "[" + CONSULT_MARKER + "]"):
                        rec["consult_checkpoints"] += 1
                        continue
                    if not first_user_text:
                        first_user_text = t[:4000]
                continue
            if etype == "assistant":
                msg = ev.get("message") or {}
                content = msg.get("content")
                blocks = content if isinstance(content, list) else [content]
                for blk in blocks:
                    if not isinstance(blk, dict) or blk.get("type") != "tool_use":
                        continue
                    name = blk.get("name") or ""
                    inp = blk.get("input") or {}
                    rec["tool_calls_total"] += 1
                    if rec["first_tool_round"] < 0:
                        rec["first_tool_round"] = round_idx
                    if is_memory_search(name, inp):
                        rec["searches"] += 1
                        if rec["first_search_round"] < 0:
                            rec["first_search_round"] = round_idx
                            rec["first_search_id"] = blk.get("id") or ""
                        search_ids.add(blk.get("id") or "")
                        if round_idx not in rec["search_rounds"]:
                            rec["search_rounds"].append(round_idx)
                        last_search_round = round_idx
                        rec["search_subject"] |= tokenize(
                            " ".join(str(inp.get(k, "")) for k in
                                     ("query", "intent")))
                        continue
                    if rec["first_search_round"] < 0:
                        rec["tool_calls_before_search"] += 1
                    if name in EXPLORE_TOOLS:
                        area = tool_input_area(name, inp)
                        if area:
                            rec["areas"].append(area)
                            areas_by_round.setdefault(round_idx, []).append(area)
                continue
            if etype in ("tool_result", "user"):
                # A memory-search result: capture the row ids it handed back so
                # "the run's first search came back EMPTY" is a measurement.
                # Only a genuine tool_result event carries tool_use_id (a
                # `user` event's content is a plain string).
                for blk in tool_result_blocks(ev):
                    if blk.get("tool_use_id") in search_ids:
                        search_rows[blk.get("tool_use_id")] = \
                            row_ids_in_result(blk.get("content"))
                continue
            if etype == "request":
                rec["round_completion"].append(int(
                    ev.get("completion_tokens", 0) or 0))
                if rec["first_search_round"] < 0:
                    rec["completion_tokens_before_search"] += int(
                        ev.get("completion_tokens", 0) or 0)
                    rec["prompt_tokens_before_search"] += int(
                        ev.get("prompt_tokens", 0) or 0)
                round_idx += 1
                round_started = True
                continue
            if etype == "session_end":
                rec["ended"] = True
                rec["exit_code"] = int(ev.get("exit_code", 0) or 0)
                rec["total_tokens"] = int(ev.get("total_tokens", 0) or 0)
                rec["elapsed_sec"] = float(ev.get("elapsed_sec", 0.0) or 0.0)
                continue
    rec["rounds"] = round_idx
    rec["search_result_rows"] = {k: v for k, v in search_rows.items()}
    rec["first_search_rows"] = (
        search_rows.get(rec["first_search_id"]) if rec["first_search_id"] else None)
    rec["explored_first"] = (rec["first_tool_round"] >= 0
                             and rec["first_search_round"] != rec["first_tool_round"])
    rec["search_subject"] = sorted(rec["search_subject"])
    rec["areas"] = rec["areas"][:200]

    # Candidate-B signature: `stuck_rounds` consecutive rounds, each carrying
    # an exploration call in the SAME area, with NO memory search issued in any
    # of those rounds — i.e. the moment a mid-task "have I done this before?"
    # nudge could have fired. A search anywhere inside the streak breaks it
    # (that is the behaviour the candidate-B hook would be trying to cause).
    search_set = set(rec["search_rounds"])
    streak = 0
    prev_area = ""
    for r in sorted(areas_by_round):
        area = areas_by_round[r][0]
        if not area or r in search_set:
            streak = 0
            prev_area = ""
            continue
        streak = streak + 1 if area == prev_area else 1
        prev_area = area
        if streak >= stuck_rounds:
            rec["stuck_explore_round"] = r
            rec["stuck_area"] = area
            break
    rec["searches_before_stuck"] = sum(
        1 for r in rec["search_rounds"]
        if rec["stuck_explore_round"] < 0 or r < rec["stuck_explore_round"])

    if rec["stuck_explore_round"] >= 0:
        after = rec["round_completion"][rec["stuck_explore_round"]:]
        rec["completion_tokens_after_stuck"] = sum(after)
        rec["rounds_after_stuck"] = len(after)
    rec["completion_tokens_total"] = sum(rec["round_completion"])
    del rec["round_completion"]

    # Subject of the run: the epic goal text + its task summary (when known).
    subject_text = first_user_text
    rec["session_subject"] = sorted(tokenize(subject_text))
    rec["_subject_text"] = subject_text
    return rec


def classify(rec, rows, stuck_rounds, area_overlap=AREA_OVERLAP_MIN):
    """Split the case into 真丢 vs 存了没想起, and decide whether a pre-existing
    applicable row was waiting (the candidate-B 'would have been useful' bit).

    A row is APPLICABLE when it predates the session and its subject overlaps
    the session's own subject by >= SUBJECT_OVERLAP_MIN tokens. `stuck_rounds`
    is the candidate-B firing threshold this run reports on (the CLI can sweep
    it, so the GO/NO-GO decision rests on a curve, not on one setting)."""
    subj = set(rec["session_subject"])
    for a in rec["areas"]:
        subj |= tokenize(a)
    applicable, consultable = [], []
    for row in rows:
        if row["distilled_at"] >= rec["start_ts"]:
            continue                      # not yet stored when the session ran
        overlap = len(row["subject"] & subj)
        if overlap < SUBJECT_OVERLAP_MIN:
            continue
        applicable.append((overlap, row))
        if row["last_retrieved"] < rec["start_ts"]:
            consultable.append((overlap, row))   # never pulled since before
    applicable.sort(key=lambda t: t[0], reverse=True)
    consultable.sort(key=lambda t: t[0], reverse=True)
    rec["applicable_rows"] = [{"id": r["id"], "overlap": o,
                               "tier": r["tier"], "sources": r["sources"][:160]}
                              for o, r in applicable[:5]]
    rec["consultable_rows"] = [r["id"] for _, r in consultable[:5]]

    forgot = (rec["explored_first"] and rec["tool_calls_before_search"] > 0)
    if rec["searches"] == 0:
        # The checkpoint's own presence in the log is what separates
        # "enforcement was off for this run" from "the harness asked and the
        # model did not search" — the epic's evidence classes must not merge.
        if rec["start_checkpoint"]:
            # The 忘記先查 class proper: the harness forced the question, the
            # run never consulted memory. Split by whether anything was there
            # to find — 存了但没想起来查 (a row existed) vs 真丢 (nothing stored).
            rec["case"] = ("never-consulted-had-row" if consultable
                           else "never-consulted-nothing-stored")
            rec["never_consulted_token_cost"] = (rec["total_tokens"]
                                                or rec["completion_tokens_total"])
        else:
            rec["case"] = "no-checkpoint-no-search"
    elif not forgot:
        rec["case"] = "started-with-search"
    elif consultable:
        rec["case"] = "stored-unconsulted"      # 存了但没想起来查
    else:
        rec["case"] = "truly-lost"              # 真丢: nothing had been stored
    # The mid-task déjà-vu leg, scored INDEPENDENTLY of the start-of-run leg:
    # a run can start with a search and still re-explore an area a stored row
    # already covered (that is the candidate-B signal).
    area_toks = tokenize(rec["stuck_area"])
    covered = []
    for row in rows:
        if rec["start_ts"] and row["distilled_at"] >= rec["start_ts"]:
            continue
        if len(row["subject"] & area_toks) >= area_overlap:
            covered.append(row)
    covered.sort(key=lambda r: (-len(r["subject"] & area_toks), r["id"]))
    rec["stuck_area_rows"] = [{"id": r["id"], "overlap":
                              len(r["subject"] & area_toks),
                              "label": r["label"],
                              "sources": r["sources"][:120],
                              "retrieval_count": r["retrieval_count"],
                              "last_retrieved": r["last_retrieved"]}
                             for r in covered[:5]]
    rec["stuck_area_unconsulted"] = [
        r["id"] for r in covered if r["last_retrieved"] < rec["start_ts"]]
    # MIXED class: the run DID consult memory (searches>0) and STILL re-explored
    # an area a stored row covered — i.e. it searched, but on a subject that
    # does not intersect the area it subsequently re-derived. Only these runs
    # are the residual gap candidate B could serve (a run that searched its own
    # area early and still re-explored means the start-of-run leg was too
    # shallow; a run that never searched at all is the start-of-run leg's
    # business, not candidate B's).
    searched_toks = set()
    for t in rec["search_subject"]:
        searched_toks |= tokenize(t)
    matched = [x for x in rec["stuck_area_rows"]
               if len(tokenize(x["label"]) & searched_toks) >= area_overlap]
    rec["stuck_area_searched_before"] = len(matched) > 0
    rec["dejavu_preventable"] = bool(
        rec["stuck_explore_round"] >= 0 and rec["stuck_area_unconsulted"])
    rec["is_work_run"] = (rec["tool_calls_total"] >= MIN_RUN_TOOL_CALLS
                          and rec["rounds"] > 0)
    return rec


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--sessions", type=int, default=DEFAULT_SESSIONS)
    ap.add_argument("--window-days", type=int, default=DEFAULT_WINDOW_DAYS,
                    help="only session logs touched within this many days")
    ap.add_argument("--out", default="")
    ap.add_argument("--db", default=MEMORY_DB)
    ap.add_argument("--area-overlap", type=int, default=AREA_OVERLAP_MIN,
                    help="subject-token overlap required for a row to count as "
                         "covering the re-explored area")
    ap.add_argument("--stuck", type=int, default=STUCK_EXPLORE_ROUNDS,
                    help="candidate-B firing threshold in consecutive "
                         "same-area rounds")
    args = ap.parse_args(argv)

    rows = load_memory_rows(args.db)
    files = newest_session_files(SESSION_FILE_GLOB, args.sessions,
                                 args.window_days)
    window_floor_ts = min(os.path.getmtime(p) for p in files) if files else 0.0

    out = []
    for p in files:
        try:
            rec = scan_session(p, rows, args.stuck)
        except (OSError, ValueError) as exc:      # never die on one bad log
            print("[audit] skip %s: %s" % (p, exc), file=sys.stderr)
            continue
        if rec is None or not rec["task_id"] and not rec["workdir"]:
            continue
        out.append(classify(rec, rows, args.stuck, args.area_overlap))

    cases = {}
    for r in out:
        cases[r["case"]] = cases.get(r["case"], 0) + 1
    work = [r for r in out if r["is_work_run"]]
    work_cases = {}
    for r in work:
        work_cases[r["case"]] = work_cases.get(r["case"], 0) + 1
    fired = [r for r in work if r["stuck_explore_round"] >= 0]
    fired_useful = [r for r in fired if r["stuck_area_rows"]]
    fired_consultable = [r for r in fired if r["stuck_area_unconsulted"]]
    # The RESIDUAL class candidate B would serve: a run that had ALREADY used
    # the start-of-run leg (searches before the streak) and still re-explored an
    # area an unconsulted stored row covered. Runs that never searched at all
    # are the start-of-run leg's failures, not candidate B's.
    residual = [r for r in fired_consultable if r["searches_before_stuck"] > 0]
    # …and the still-stricter class: the row the run re-derived was ALREADY
    # RETRIEVED at some point before the run (retrieval_count>0) — the literal
    # "we looked it up once and forgot" (the Bond case: our own memory-sync
    # code re-explored round after round).
    forgotten = [r for r in residual
                 if any(x["retrieval_count"] > 0 for x in r["stuck_area_rows"])]
    explored = [r for r in work if r["searches"]
                and r["explored_first"] and r["tool_calls_before_search"] > 0]
    never = [r for r in work if not r["searches"] and r["start_checkpoint"]]
    never_had_row = [r for r in never if r["case"] == "never-consulted-had-row"]
    wasted_tokens = sum(r["completion_tokens_before_search"] for r in explored)
    wasted_calls = sum(r["tool_calls_before_search"] for r in explored)
    # The whole-run cost of the worked-first-then-searched population, plus the
    # post-streak spend of the candidate-B population (the upper bound on what
    # a mid-task nudge could have saved — it is an upper bound because a nudge
    # only helps when the row answers the question).
    post_streak_calls = sum(
        sum(1 for a in r["areas"]) for r in fired_consultable)
    summary = {
        "sessions_scanned": len(out),
        "work_runs": len(work),
        "window_floor_ts": window_floor_ts,
        "cases": cases,
        "cases_work_runs": work_cases,
        "checkpoint_fired": sum(1 for r in out if r["start_checkpoint"]),
        # The 忘記先查 population (D3's headline): the harness asked the
        # start-of-run question and the run never consulted memory. Cost is the
        # whole run's tokens, since the hook it needed was the one that never ran.
        "never_consulted_runs": len(never),
        "never_consulted_with_row_waiting": len(never_had_row),
        "never_consulted_token_cost": sum(
            r["never_consulted_token_cost"] for r in never),
        "never_consulted_token_cost_had_row": sum(
            r["never_consulted_token_cost"] for r in never_had_row),
        "consult_checkpoint_events": sum(r["consult_checkpoints"] for r in out),
        "consult_checkpoint_runs": sum(
            1 for r in out if r["consult_checkpoints"]),
        "checkpoint_fired_but_no_search": sum(
            1 for r in out if r["start_checkpoint"] and not r["searches"]),
        "explored_before_consult": len(explored),
        "wasted_tool_calls_before_search": wasted_calls,
        "wasted_completion_tokens_before_search": wasted_tokens,
        "candidate_b_threshold_rounds": args.stuck,
        "candidate_b_area_overlap_min": args.area_overlap,
        "candidate_b_fired": len(fired),
        "candidate_b_fired_with_applicable_row": len(fired_useful),
        "candidate_b_fired_with_unconsulted_row": len(fired_consultable),
        "candidate_b_hit_rate": (len(fired_consultable) / len(fired)) if fired else 0.0,
        "candidate_b_post_stuck_completion_tokens": sum(
            r["completion_tokens_after_stuck"] for r in fired),
        "candidate_b_post_stuck_rounds": sum(
            r["rounds_after_stuck"] for r in fired),
        # Firing split by evidence class: a mid-task hook is only a RESIDUAL
        # gap for runs the start-of-run leg already covered (searches>0).
        "candidate_b_fired_search_started": sum(
            1 for r in fired if r["searches"] > 0),
        "candidate_b_fired_search_started_unconsulted": sum(
            1 for r in fired if r["searches"] > 0
            and r["stuck_area_unconsulted"]),
        "candidate_b_post_stuck_completion_tokens_search_started": sum(
            r["completion_tokens_after_stuck"] for r in fired
            if r["searches"] > 0),
        # STRICT class — the literal 忘記先查: the run had issued NO memory
        # search at all before the streak, and an unconsulted covering row was
        # waiting. This is the population candidate B would actually serve; the
        # loose class (any unconsulted covering row) also counts runs that had
        # already searched, where the injected question is answered "already
        # done" and the injection is pure noise.
        "candidate_b_fired_never_searched": sum(
            1 for r in fired if r["searches_before_stuck"] == 0),
        "candidate_b_fired_never_searched_unconsulted": sum(
            1 for r in fired if r["searches_before_stuck"] == 0
            and r["stuck_area_unconsulted"]),
        # The population that decides GO/NO-GO: residual (the start-of-run leg
        # had already run and the run still re-derived a stored answer) and
        # forgotten (that answer had been retrieved before, so the knowledge was
        # demonstrably known and demonstrably unused).
        "candidate_b_residual_runs": len(residual),
        "candidate_b_forgotten_runs": len(forgotten),
        "candidate_b_residual_hit_rate": (
            len(residual) / len(fired)) if fired else 0.0,
        "candidate_b_post_streak_explore_calls": post_streak_calls,
        "candidate_b_never_searched_hit_rate": (
            sum(1 for r in fired if r["searches_before_stuck"] == 0
                and r["stuck_area_unconsulted"])
            / sum(1 for r in fired if r["searches_before_stuck"] == 0)
        ) if any(r["searches_before_stuck"] == 0 for r in fired) else 0.0,
        # ── the ZERO-HIT population (candidate B's replacement hypothesis) ──
        # Measured here rather than assumed: the run's FIRST memory search came
        # back with no distilled row (first_search_rows == []), then the run
        # went on to hit a same-area exploration streak. `…_with_unconsulted_row`
        # is the would-have-been-useful bit; a trigger whose population has no
        # covering row is a noise generator (epic constraint 4).
        "zero_hit_first_search_runs": sum(
            1 for r in work if r["first_search_rows"] == []),
        "zero_hit_first_search_with_streak": sum(
            1 for r in work if r["first_search_rows"] == []
            and r["stuck_explore_round"] >= 0),
        "zero_hit_first_search_with_unconsulted_row": sum(
            1 for r in work if r["first_search_rows"] == []
            and r["stuck_explore_round"] >= 0 and r["stuck_area_unconsulted"]),
        "zero_hit_post_streak_completion_tokens": sum(
            r["completion_tokens_after_stuck"] for r in work
            if r["first_search_rows"] == [] and r["stuck_explore_round"] >= 0),
        # Non-empty first search, for contrast: the population the zero-hit
        # trigger would NOT fire on.
        "nonempty_first_search_runs": sum(
            1 for r in work if r["first_search_rows"]),
        "first_search_result_unknown": sum(
            1 for r in work if r["searches"] and r["first_search_rows"] is None),
    }
    payload = {"summary": summary, "sessions": out}
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            json.dump(payload, fh, ensure_ascii=False)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    print("\n-- case list (session, case, pre-search tool calls, "
          "pre-search completion tokens, consultable rows) --")
    for r in sorted(work, key=lambda r: -r["tool_calls_before_search"])[:60]:
        if not (r["explored_first"] and r["tool_calls_before_search"] > 0
                and r["searches"]):
            continue
        print("%s  %-20s calls=%-4d comp=%-7d rounds=%-4d stuck@%-4d %s" % (
            r["session"][:12], r["case"], r["tool_calls_before_search"],
            r["completion_tokens_before_search"], r["rounds"],
            r["stuck_explore_round"], r["consultable_rows"]))
    print("\n-- 忘記先查: harness asked, run never consulted memory (top cost) --")
    for r in sorted(never, key=lambda r: -r["never_consulted_token_cost"])[:30]:
        print("%s  %-26s tokens=%-9d rounds=%-4d tools=%-4d rows_waiting=%s" % (
            r["session"][:12], r["case"], r["never_consulted_token_cost"],
            r["rounds"], r["tool_calls_total"], r["consultable_rows"][:4]))
    print("\n-- candidate-B firing (déjà-vu) signature (threshold %d rounds) --"
          % args.stuck)
    for r in sorted(fired, key=lambda r: -r["completion_tokens_after_stuck"]):
        print("%s area=%-34s stuck@%-4d rounds_after=%-4d comp_after=%-8d "
              "cover_rows=%s unconsulted=%s" % (
                  r["session"][:12], r["stuck_area"][:34],
                  r["stuck_explore_round"], r["rounds_after_stuck"],
                  r["completion_tokens_after_stuck"],
                  ["#%d %s" % (x["id"], x["label"][:48])
                   for x in r["stuck_area_rows"]],
                  r["stuck_area_unconsulted"][:3]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
