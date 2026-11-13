#!/usr/bin/env python3
"""Post-hoc scan of the D1 review-time consult checkpoint (the audit instrument).

The checkpoint's mechanism claim ("it cannot be skipped; it is written to the
session JSONL") is cheap to STATE and expensive to TRUST. This script measures
the part of it that is deterministic, over a bounded window:

  * how many real runs carry the marker, and how many times (one-shot proof);
  * whether each of those runs actually produced an assistant turn AFTER the
    marker (a skipped checkpoint would show a marker with no following reply);
  * the marker-to-reply distance, and the reply's opening text, so the ANSWER
    can be read by hand.

Fixture exclusion: a `go test` run of internal/agent writes marker sessions too
(model "test-model", workdir under internal/agent). They are counted separately
because their population grows on every test run and would otherwise inflate
"real runs".

WHAT THIS SCRIPT DELIBERATELY DOES NOT DO — classify the answer. A first version
did (EXPLICIT-NONE vs EXPLICIT-WORK by substring) and produced four
"EXPLICIT-NONE" replies whose `none` was about the TASK LIST, not the checkpoint
("none are progressing"). Free-prose classification by substring is not a
measurement; the yield of the checkpoint is a reading, reported as such in
devdoc/memory-consult.md, never a ratio this script prints.

Bounded by construction: newest --sessions logs inside a --window-days mtime
window, each read once. Stdlib only, read-only — it never writes the sessions
tree or memory.db.

Usage:
  python3 scripts/memconsult-checkpoint-scan.py [--sessions 400] [--window-days 14]
"""

import argparse
import glob
import json
import os
import sys
import time

SESSIONS_GLOB = os.path.expanduser("~/.gopi/sessions/*/session.jsonl")
MARKER = "MEMORY-CONSULT CHECKPOINT"
FIXTURE_MODEL = "test-model"
FIXTURE_WORKDIR = "internal/agent"
DEFAULT_SESSIONS = 400
DEFAULT_WINDOW_DAYS = 14
EXCERPT_CHARS = 150


def assistant_text(record):
    """Return the record's assistant-visible text (thinking + text), or ''."""
    message = record.get("message")
    content = message.get("content") if isinstance(message, dict) else None
    if content is None:
        content = record.get("content")
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return ""
    parts = []
    for block in content:
        if not isinstance(block, dict):
            continue
        for kind in ("thinking", "text"):
            if isinstance(block.get(kind), str):
                parts.append(block[kind])
    return " ".join(parts)


def injection_indices(lines):
    """Line indices of MARKER *INJECTIONS* — the harness's user-message events.

    NOT a substring count. The LLM quotes the marker back when it answers
    ("[MEMORY-CONSULT CHECKPOINT] ... none"), so `grep -c '<marker>'` on a
    session log returns 1 injection + N answer quotes: the cost-probe arm
    `43888439` measures 3 by grep and exactly 1 by this function (5th
    measurement defect this report has caught — the instrument, not the
    mechanism). An injection is a USER-message event whose text carries the
    marker; an assistant event carrying it is the run quoting its own prompt.
    """
    out = []
    for index, line in enumerate(lines):
        if MARKER not in line:
            continue
        try:
            record = json.loads(line)
        except ValueError:
            continue
        message = record.get("message")
        content = message.get("content") if isinstance(message, dict) else None
        if isinstance(content, str):
            # Injection SHAPE: the marker begins the message. A substring test
            # also matches a run (or a work-area echo) QUOTING the marker —
            # defect 7, seen as an audit reporting 291 marker runs in a window
            # with 289 work runs.
            if record.get("type") == "user" and \
                    content.lstrip().startswith("[" + MARKER + "]"):
                out.append(index)
            continue
        if isinstance(content, list):
            for block in content:
                if not isinstance(block, dict):
                    continue
                if block.get("type") == "text" and \
                        str(block.get("text", "")).lstrip().startswith(
                            "[" + MARKER + "]"):
                    if record.get("role") == "user":
                        out.append(index)
                    break
    return out


def scan(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            lines = fh.read().splitlines()
    except OSError:
        return None
    marker_at = injection_indices(lines)
    if not marker_at:
        return None
    # A run that quotes the marker without having been injected it would be a
    # reader bug, not a run: report the quote count so the two can never be
    # silently confused again.
    quoted = sum(1 for line in lines if MARKER in line) - len(marker_at)
    head = " ".join(lines[:8])
    marker_index = marker_at[0]
    reply_index = -1
    reply = ""
    for index in range(marker_index + 1, len(lines)):
        try:
            record = json.loads(lines[index])
        except ValueError:
            continue
        text = assistant_text(record)
        if text.strip():
            reply_index = index
            reply = text
            break
    return {
        # The FULL session id: this field is the report's identity for the run,
        # and a truncated one is ambiguous (and cannot be resolved by
        # `gopi log <id>`, which refuses a prefix).
        "session": os.path.basename(os.path.dirname(path)),
        "fixture": FIXTURE_MODEL in head or FIXTURE_WORKDIR in head,
        "marker_count": len(marker_at),
        "quote_count": quoted,
        "marker_line": marker_index + 1,
        "reply_line": reply_index + 1 if reply_index >= 0 else 0,
        "reply_delta": reply_index - marker_index if reply_index >= 0 else -1,
        "reply_chars": len(reply),
        "excerpt": reply[:EXCERPT_CHARS].replace("\n", " "),
    }


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--sessions", type=int, default=DEFAULT_SESSIONS)
    parser.add_argument("--window-days", type=float, default=DEFAULT_WINDOW_DAYS)
    parser.add_argument("--json", help="write the raw rows here")
    args = parser.parse_args(argv)

    now = time.time()
    window = args.window_days * 86400
    logs = []
    for path in glob.glob(SESSIONS_GLOB):
        try:
            mtime = os.path.getmtime(path)
        except OSError:
            continue
        if args.window_days == 0 or now - mtime <= window:
            logs.append((mtime, path))
    if args.sessions:
        logs = sorted(logs, reverse=True)[:args.sessions]

    rows = [r for r in (scan(path) for _, path in logs) if r]
    real = [r for r in rows if not r["fixture"]]
    fixtures = [r for r in rows if r["fixture"]]
    counts = sorted(r["marker_count"] for r in rows) or [0]

    print(f"scanned_logs={len(logs)}  window_days={args.window_days}")
    print(f"marker_logs={len(rows)}  fixture_logs={len(fixtures)}  real_runs={len(real)}")
    quotes = sorted(r["quote_count"] for r in rows) or [0]
    print(f"marker_occurrences min/median/max={counts[0]}/{counts[len(counts) // 2]}/{counts[-1]}"
          "   (one-shot = every run carries it exactly once; an injection only —"
          " the run's own quotes of the marker are counted separately)")
    print(f"marker_QUOTES min/median/max={quotes[0]}/{quotes[len(quotes) // 2]}/{quotes[-1]}"
          "   (grep -c counts injection+quotes; that is why the probe's numbers"
          " disagreed with grep)")
    print(f"real_runs_with_a_reply_after_the_marker="
          f"{sum(1 for r in real if r['reply_line'])}/{len(real)}"
          "   (a skipped checkpoint would be marker-with-no-reply)")
    print(f"marker->reply line delta: {sorted(r['reply_delta'] for r in real if r['reply_line'])}")
    print("")
    for r in sorted(real, key=lambda r: r["session"]):
        print(f"{r['session']}  marker@{r['marker_line']:>3} reply@{r['reply_line']:>3}"
              f" (+{r['reply_delta']})  {r['reply_chars']} chars: {r['excerpt']}")
    if args.json:
        with open(args.json, "w", encoding="utf-8") as fh:
            json.dump(rows, fh, ensure_ascii=False, indent=1)
    return 0


if __name__ == "__main__":
    sys.exit(main())
