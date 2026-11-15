#!/usr/bin/env python3
"""arm-a-cdp-probe.py — the MISSING cell of the 2x2: CDP actuation at a chosen IP family.

The A/B has 4 cells:
    actuation \ family |  IPv6            |  IPv4
    CDP (Arm A)        |  /sorry          |  ???  <- THIS script
    X11 (Arms B/C)     |  /sorry          |  full SERP

Arm A (production) is IPv6 + CDP. Arms B/C are X11-only. To decide whether the
ACTUATION METHOD or the EGRESS FAMILY is the binding constraint, the last cell
(CDP at IPv4) must be measured with the same instrument as Arm A.

Instrument: the production gateway's own masked-WebSocket CDP client
(`CDPConn` from google_cdp_server.py) — reusing it keeps Arm A's measurement
identical across cells: Runtime.evaluate to navigate, Runtime.evaluate to read
the DOM. No new dependency, no second implementation.

Usage:
    python3 arm-a-cdp-probe.py [--cdp-port 9222] [--query "..."] [--label A-IPV6]

Output: one JSON object — {label, cdp_port, nav_url, landed_href, title,
body_head, h3_titles, sorry, enablejs, elapsed_s}. `sorry`/`enablejs` are the
challenge markers, `h3_titles` the result signal: the same shape the A/B table
uses for every other cell.
"""
import argparse
import json
import os
import sys
import time
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from google_cdp_server import CDPConn, cdp_eval, get_page_target  # noqa: E402

PAGE_LOAD_WAIT = 4.0
DOM_SETTLE_WAIT = 3.0
BODY_HEAD_CHARS = 1200
WARMUP_URL = "https://www.google.com/"


def read_state(conn):
    """The per-cell signal: landed URL, title, challenge markers, result titles."""
    landed = cdp_eval(conn, "location.href", budget=6) or ""
    title = cdp_eval(conn, "document.title", budget=6) or ""
    body = cdp_eval(conn, "(document.body&&document.body.innerText||'').slice(0,%d)"
                    % BODY_HEAD_CHARS, budget=8) or ""
    h3s = cdp_eval(
        conn,
        "JSON.stringify(Array.prototype.slice.call(document.querySelectorAll('h3'))"
        ".slice(0,10).map(function(h){return h.textContent.trim()}))",
        budget=8) or "[]"
    try:
        h3_titles = json.loads(h3s)
    except ValueError:
        h3_titles = []
    low = body.lower()
    return {
        "landed_href": landed,
        "title": title,
        "body_head": body,
        "h3_titles": h3_titles,
        "sorry": int("sorry/index" in landed or "unusual traffic" in low
                     or "not a robot" in low),
        "enablejs": int("enablejs" in landed or "enablejs" in low),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cdp-port", type=int, default=9222)
    ap.add_argument("--query", default="golang context deadline")
    ap.add_argument("--label", default="A")
    ap.add_argument("--warmup", action="store_true",
                    help="navigate to the google.com homepage first and let it "
                         "settle before the search URL — the human-like warm-up "
                         "path the X11 arms use (they Ctrl+L FROM the homepage). "
                         "Without it the probe DEEP-LINKS straight to /search.")
    args = ap.parse_args()

    # the module hardcodes CDP_PORT in its helpers; point them at the requested port
    import google_cdp_server as gw
    gw.CDP_PORT = args.cdp_port

    nav_url = "https://www.google.com/search?" + urllib.parse.urlencode(
        {"q": args.query, "hl": "en", "gl": "us"})
    out = {"label": args.label, "cdp_port": args.cdp_port, "nav_url": nav_url}
    t0 = time.time()
    try:
        ws = get_page_target()
        if not ws:
            raise RuntimeError("no page target on :%d" % args.cdp_port)
        path = ws.replace("ws://localhost:%d" % args.cdp_port, "").replace(
            "ws://127.0.0.1:%d" % args.cdp_port, "")
        if not path.startswith("/"):
            path = ws
        conn = CDPConn(path)
        cdp_eval(conn, "1+1", budget=6)
        if args.warmup:
            # warm-up leg: homepage first, settle, THEN the search URL — matches
            # the X11 arms, which land on google.com and Ctrl+L from there.
            try:
                cdp_eval(conn, "location.href=%s;" % json.dumps(WARMUP_URL), budget=5)
            except Exception:
                pass
            time.sleep(PAGE_LOAD_WAIT)
            out["warmup_title"] = cdp_eval(conn, "document.title", budget=6)
            out["warmup_href"] = cdp_eval(conn, "location.href", budget=6)
        try:
            cdp_eval(conn, "location.href=%s;" % json.dumps(nav_url), budget=5)
        except Exception:
            pass                     # the navigation destroys the eval context
        time.sleep(PAGE_LOAD_WAIT)
        out.update(read_state(conn))
        if not out.get("h3_titles"):
            time.sleep(DOM_SETTLE_WAIT)
            out.update(read_state(conn))
        about = cdp_eval(conn, "document.body?document.body.innerText.length:0", budget=6)
        out["body_chars"] = about
    except Exception as e:
        out["error"] = repr(e)
    out["elapsed_s"] = round(time.time() - t0, 1)
    print(json.dumps(out, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main())
