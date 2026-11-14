#!/usr/bin/env python3
"""flowwrapmock.py — an OFFLINE, deterministic Anthropic-SSE stub for the
over-wide agent flow row capture (devdoc/evidence/sgr-grid/capture-flowwrap.sh).

Why it exists and why it is not fakellm.py: fakellm's `think` scenario streams a
thinking block of fixed SHORT lines, and capture-flow.sh drives the REAL endpoint
(it needs a live reasoning stream and a clamped-budget notice). This capture
needs the opposite properties — no network spend, and a reasoning block of a
KNOWN length, long enough that the reserved band must wrap it several times at
100 columns — so the shot is anchored on the stub's own text rather than on a
model's choice of words.

Wire format: identical to fakellm.py (same SSE event shape, chunked transfer), so
the app exercises its real agent loop and its real renderer.

Usage: flowwrapmock.py <port> <markfile>

The reasoning text is one row of FILLCELLS cells: it starts with the app's own
dim marker (the agent opens the reasoning block with `ESC[2m[thinking] `) and the
body is a repeated fixed token, so the wrap boundaries are arithmetic rather than
luck. The mark file records each request's 1-based index and state, which is how
a caller waits on an OBSERVED request instead of a clock.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1])
MARK = sys.argv[2] if len(sys.argv) > 2 else ""

# FILLCELLS — how many CELLS the stub's reasoning row carries. It must be several
# times the 100-column capture width, or the band never has to decide where the
# row breaks (which is the state this capture photographs). A named constant
# because it is the capture's premise, not an incidental number.
FILLCELLS = 260

# FILLTOKEN — the body unit. ASCII, so its display width equals its length and
# the expected wrap boundaries can be computed by hand for the assertion.
FILLTOKEN = "flowwrap"

# DELTA_GAP — the gap between reasoning deltas. The status row goes live on the
# first delta, and the whole row must have ARRIVED before the shot; a stream that
# closes instantly leaves the anchor unobservable.
DELTA_GAP = 0.25

REASONING = (FILLTOKEN * ((FILLCELLS // len(FILLTOKEN)) + 1))[:FILLCELLS]
ANSWER = "Done. flowwrap capture complete.\n"

# ANCHOR — the substring the capture's `waitfor` op anchors on. It is the END of
# the reasoning row: when it is on screen, the WHOLE row has been rendered and
# wrapped, so the shot cannot land mid-arrival. The caller passes it in the
# environment so the script and the stub cannot drift apart.
ANCHOR = REASONING[-len(FILLTOKEN):]


def mark(idx, state):
    if not MARK:
        return
    with open(MARK, "w") as f:
        f.write("%d %s\n" % (idx, state))


def sse(w, etype, payload):
    body = ("event: %s\ndata: %s\n\n" % (etype, payload)).encode()
    w.write(b"%x\r\n" % len(body) + body + b"\r\n")
    w.flush()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(n)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        w = self.wfile
        # The reasoning row, streamed in pieces so the live display really is
        # live: the first piece opens the block, the last one closes it.
        sse(w, "message_start",
            '{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}')
        sse(w, "content_block_start",
            '{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}')
        part = max(1, len(REASONING) // 8)
        for i in range(0, len(REASONING), part):
            sse(w, "content_block_delta",
                json.dumps({"type": "content_block_delta", "index": 0,
                            "delta": {"type": "thinking_delta",
                                      "thinking": REASONING[i:i + part]}}))
            time.sleep(DELTA_GAP)
        sse(w, "content_block_delta",
            json.dumps({"type": "content_block_delta", "index": 0,
                        "delta": {"type": "signature_delta", "signature": "sig"}}))
        sse(w, "content_block_stop", '{"type":"content_block_stop","index":0}')
        sse(w, "content_block_start",
            '{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}')
        sse(w, "content_block_delta",
            json.dumps({"type": "content_block_delta", "index": 1,
                        "delta": {"type": "text_delta", "text": ANSWER}}))
        sse(w, "content_block_stop", '{"type":"content_block_stop","index":1}')
        sse(w, "message_delta",
            '{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}')
        sse(w, "message_stop", '{"type":"message_stop"}')
        w.write(b"0\r\n\r\n")
        w.flush()
        mark(1, "done")


if __name__ == "__main__":
    sys.stderr.write("flowwrapmock on %d anchor=%s\n" % (PORT, ANCHOR))
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
