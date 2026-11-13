#!/usr/bin/env python3
"""seed_web.py — put a REAL turn into `gopi web` before it is photographed.

WHY. A capture of an empty chat page proves the page loads; it does not prove
the interface is USED. The difference is the same one the materials corpus drew
between its "refusal path" entries and its "really in use" ones: nobody learns
anything from a picture of an idle input box.

HOW. The page's own endpoint, not a back door: POST /api/chat is exactly what
the browser calls, and it is deliberately the way in (a same-host request
carries the local shell's trust, which is why no token is needed here). The
server owns the transcript, so a reload replays the answer — which is why
`gopi shot` afterwards photographs a conversation rather than an empty page.

USAGE
  seed_web.py --addr 127.0.0.1:18097 --prompt "..." [--timeout-sec 120] [--json-out PATH]
Exit 0 only when a NON-EMPTY assistant turn is in the transcript; a page that
was photographed before the answer arrived is the failure this guards.
"""
import argparse
import json
import sys
import time
import urllib.error
import urllib.request


def post_chat(addr, prompt, timeout):
    body = json.dumps({"message": prompt, "history": []}).encode("utf-8")
    req = urllib.request.Request(
        "http://%s/api/chat" % addr, data=body,
        headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read().decode("utf-8", "replace")


def transcript(addr):
    with urllib.request.urlopen("http://%s/api/transcript" % addr, timeout=20) as resp:
        return json.loads(resp.read().decode("utf-8", "replace"))


def assistant_is_nonempty(tr):
    """True when the transcript holds a non-blank assistant turn."""
    items = tr if isinstance(tr, list) else tr.get("messages") or tr.get("entries") or []
    for m in items:
        role = (m.get("role") or "").lower()
        content = (m.get("content") or "").strip()
        if role == "assistant" and content:
            return True, content
    return False, ""


def main():
    p = argparse.ArgumentParser(add_help=True)
    p.add_argument("--addr", required=True)
    p.add_argument("--prompt", required=True)
    p.add_argument("--timeout-sec", type=int, default=180)
    p.add_argument("--json-out", default="")
    args = p.parse_args()

    deadline = time.time() + args.timeout_sec
    started = time.time()
    stream = post_chat(args.addr, args.prompt, args.timeout_sec)
    elapsed = time.time() - started
    # The turn is only DONE when the server's own transcript says so: the POST
    # response can arrive before the assistant message is committed, and a
    # capture taken in that window shows the page mid-turn.
    answer = ""
    while time.time() < deadline:
        try:
            ok, answer = assistant_is_nonempty(transcript(args.addr))
        except (urllib.error.URLError, ValueError, OSError):
            ok = False
        if ok:
            break
        time.sleep(1.0)
    if not answer:
        sys.exit("seed_web: no non-empty assistant turn in the transcript after %.0fs — "
                 "a capture now would photograph an idle page" % (time.time() - started))
    record = {"addr": args.addr, "prompt": args.prompt, "answer": answer,
              "turn_seconds": round(elapsed, 2), "stream_bytes": len(stream)}
    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as fh:
            json.dump(record, fh, ensure_ascii=False, indent=2, sort_keys=True)
    print(json.dumps({k: record[k] for k in ("turn_seconds", "stream_bytes")}, ensure_ascii=False))


if __name__ == "__main__":
    main()
