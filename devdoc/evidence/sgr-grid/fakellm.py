#!/usr/bin/env python3
"""fakellm.py — a scripted Claude/Anthropic-SSE mock for UI capture.

Why this exists: capturing `gopi chat --tools`'s screen needs a REAL
tool-calling turn, and a real turn costs money and is non-deterministic.
This server speaks the same wire format the mock in internal/testutil
(mockllm.go) speaks — same SSE frames, same tool_use shape — so the capture
exercises the real agent loop and the real renderer with zero API spend.

Usage: fakellm.py <port> <scenario> [<bodylog>]
Env:
  FAKELLM_HOLD_SEC  how long the 2nd call stays open (default 3.0)
  FAKELLM_MARK      file the request index is written to after each request
Scenarios:
  tool/text were merged into the single `styled` scenario: EVERY scenario makes
  the first request a `read` tool_use, so the capture always has a real tool
  turn. Measured 2026-09-19: the old `styled` skipped it (`SCENARIO == "tool"
  and idx == 1`), so the "toolrunning" shot photographed an idle row — a caption
  that described a state the mock never produced.
  styled — request 1: a read tool_use; request 2+: the answer + an SGR-coloured
           line emitted BY THE MODEL (ps1)
"""
import json
import os
import sys
import time
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1])
SCENARIO = sys.argv[2] if len(sys.argv) > 2 else "tool"
BODYLOG = sys.argv[3] if len(sys.argv) > 3 else ""

REQS = 0
LOCK = threading.Lock()

# HOLD_SEC — how long the model's 2nd call stays open, i.e. how long the state
# "the turn is in flight" exists and can be photographed. It is a named const
# with an env override (Rule 1a) because the capture harness must be able to
# widen it: measured 2026-09-19, the X11 instrument needs ~7 s merely to TYPE
# the prompt, so the old fixed 3.0 s window was already over before its
# `xdotool type` finished and its "toolrunning" shot photographed the ANSWER
# instead — two byte-identical PNGs, one of them mislabeled.
# An EMPTY value means "not configured", like an unset one: measured 2026-09-19,
# a blank `FAKELLM_HOLD_SEC=` in the ambient environment crashed this stub at
# import time (float('') → ValueError), and the capture then showed an app that
# never answered — a harness failure wearing the app's clothes.
_hold = os.environ.get("FAKELLM_HOLD_SEC", "").strip()
HOLD_SEC = float(_hold) if _hold else 3.0

# ANSWER_LINE_GAP — the gap between answer deltas (seconds). See text_stream:
# the stream must stay open long enough for the in-flight state to be an
# observable fact rather than a microsecond.
_gap = os.environ.get("FAKELLM_LINE_GAP", "").strip()
ANSWER_LINE_GAP = float(_gap) if _gap else 0.4

# MARK_PATH — after every request the stub writes its 1-based index here, so a
# capture WAITS for an observed state ("the 2nd call has arrived") instead of
# guessing with a sleep. The guess is what produced the mislabeled shot above.
MARK_PATH = os.environ.get("FAKELLM_MARK", "")

# TOOL_PATH — the file the scripted model's `read` call names. It is a path the
# SCREEN SHOWS (the tool banner prints it), so it belongs to the capture's
# reading matter rather than to this host: without an override the harness only
# runs where that literal exists. The default is the path the committed baseline
# was captured with, so the evidence stays faithful to what produced it.
TOOL_PATH = os.environ.get("FAKELLM_TOOL_PATH", "").strip() or \
    "/home/user/work/GoPi/devdoc/chat-mode-report.md"


CUR_IDX = 0


def mark(idx, state):
    """Record the stub's observed state for the capture harness to anchor on.

    `arrived` = the request was served; `done` = its SSE stream is complete and
    closed. Anchoring shots on these two facts is what removes the guessed
    sleeps that mislabeled a shot (measured 2026-09-19).
    """
    if not MARK_PATH:
        return
    with open(MARK_PATH, "w") as f:
        f.write("%d %s\n" % (idx, state))

LONG_ANSWER = (
    "Here is the answer with a deliberately long unwrapped paragraph so the "
    "renderer's wrapping behaviour is visible in the capture: the quick brown "
    "fox jumps over the lazy dog, and then it does it again and again until "
    "the line would certainly have to wrap somewhere near the terminal edge.\n"
    "\n"
    "A short list:\n"
    "  1. first item\n"
    "  2. second item\n"
    "  3. third item with a much longer body so it wraps past the right edge too\n"
    "\n"
    "A fenced block:\n"
    "```go\n"
    "func main() { fmt.Println(\"hello\") }\n"
    "```\n"
    "\n"
    "Done.\n"
)



# STYLED is appended to the answer by the `styled` scenario: the MODEL emits
# SGR sequences inside its text delta, so the capture shows whether model-authored
# control bytes reach the terminal's cell grid verbatim (they do: the chat
# renderer passes the streamed text through and only strips CR). The line is
# also the capture's second two-colour sample — one produced by the model, one
# injected by the instrument (`probe` in termgrid.py).
STYLED = (
    "\x1b[31mRED-ON-DEFAULT\x1b[0m and \x1b[1;2mBOLD-DIM\x1b[0m from the model.\n"
)

LONG_THINKING = (
    "The user asks what is in the file. I should read it.\n"
    "The path is devdoc/chat-mode-report.md, relative to the repo root, and the "
    "read tool resolves relative paths against the working directory, so one "
    "call is enough and a search would be wasted work.\n"
    "Then I answer briefly.\n"
)

DELTALOG = os.environ.get("FAKELLM_DELTALOG", "")


def sse(w, etype, payload):
    if DELTALOG:
        with open(DELTALOG, "a") as f:
            f.write("EV %s %s\n" % (etype, payload))

    body = ("event: %s\ndata: %s\n\n" % (etype, payload)).encode()
    w.write(body)
    w.flush()


def text_stream(w, text):
    sse(w, "message_start", '{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}')
    sse(w, "content_block_start", '{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}')
    # DRIP the answer, one line per delta with a gap. Not decoration: the app's
    # status row goes live ("thinking…") only when the FIRST delta arrives, so a
    # stub that pushes the whole text in two back-to-back deltas leaves the
    # in-flight state existing for microseconds — measured 2026-09-19, a capture
    # could not photograph it at all, and its "toolrunning" shot came out
    # byte-identical to its "answer" shot. A real streamed answer stays open for
    # as long as it takes to arrive; this gap is what makes that state (and any
    # intermediate cut of the answer) observable.
    for i, line in enumerate(text.splitlines(keepends=True)):
        sse(w, "content_block_delta",
            json.dumps({"type": "content_block_delta", "index": 0,
                        "delta": {"type": "text_delta", "text": line}}))
        if i == 0:
            # The FIRST delta is the moment the app's status row goes live. A
            # shot captioned "the turn is in flight" must be anchored HERE, on
            # this observed fact — not on the request having arrived (at that
            # instant the app is still showing "ready").
            mark(CUR_IDX, "streaming")
        time.sleep(ANSWER_LINE_GAP)
    sse(w, "content_block_stop", '{"type":"content_block_stop","index":0}')
    sse(w, "message_delta", '{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}')
    sse(w, "message_stop", '{"type":"message_stop"}')



def thinking_stream(w, thinking, text):
    """A round that streams a THINKING block first, then the answer — the shape
    a real reasoning endpoint sends, and the only way to see where the live
    reasoning display lands on screen."""
    sse(w, "message_start", '{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}')
    sse(w, "content_block_start",
        '{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}')
    for part in (thinking[:len(thinking)//2], thinking[len(thinking)//2:]):
        if part:
            sse(w, "content_block_delta",
                json.dumps({"type": "content_block_delta", "index": 0,
                            "delta": {"type": "thinking_delta", "thinking": part}}))
    sse(w, "content_block_delta",
        json.dumps({"type": "content_block_delta", "index": 0,
                    "delta": {"type": "signature_delta", "signature": "sig"}}))
    sse(w, "content_block_stop", '{"type":"content_block_stop","index":0}')
    sse(w, "content_block_start",
        '{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}')
    mid = max(1, len(text) // 2)
    for part in (text[:mid], text[mid:]):
        if part:
            sse(w, "content_block_delta",
                json.dumps({"type": "content_block_delta", "index": 1,
                            "delta": {"type": "text_delta", "text": part}}))
    sse(w, "content_block_stop", '{"type":"content_block_stop","index":1}')
    sse(w, "message_delta", '{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}')
    sse(w, "message_stop", '{"type":"message_stop"}')


def tool_stream(w, tc_id, name, args):
    args_json = json.dumps(args)
    sse(w, "message_start", '{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}')
    sse(w, "content_block_start",
        json.dumps({"type": "content_block_start", "index": 0,
                    "content_block": {"type": "tool_use", "id": tc_id, "name": name, "input": {}}}))
    sse(w, "content_block_delta",
        json.dumps({"type": "content_block_delta", "index": 0,
                    "delta": {"type": "input_json_delta", "partial_json": args_json}}))
    sse(w, "content_block_stop", '{"type":"content_block_stop","index":0}')
    sse(w, "message_delta", '{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}')
    sse(w, "message_stop", '{"type":"message_stop"}')


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def do_POST(self):
        global REQS
        n = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(n)
        with LOCK:
            REQS += 1
            idx = REQS
        global CUR_IDX
        CUR_IDX = idx
        if BODYLOG:
            with open(BODYLOG, "a") as f:
                f.write("=== request %d %s\n%s\n" % (idx, self.path, body.decode("utf-8", "replace")))
        if MARK_PATH:
            mark(idx, "arrived")
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        w = self.wfile

        def chunk(data):
            w.write(b"%x\r\n" % len(data) + data + b"\r\n")
            w.flush()

        class _C:
            def write(self, data):
                chunk(data)

            def flush(self):
                pass

        c = _C()
        if idx == 1:
            # The tool's file_path is the capture's own reading matter, not a
            # fact about this host: it is a PATH THE SCREEN SHOWS, so it must
            # be overridable (FAKELLM_TOOL_PATH) or the instrument only runs
            # where that literal happens to exist. Default is the path the
            # committed baseline was captured with — the evidence stays faithful.
            tool_stream(c, "call_01", "read", {"path": TOOL_PATH})
        else:
            # HOLD the round open so a capture can land mid-turn: the status
            # row's live state ("thinking…") only exists while a turn is in
            # flight, and a capture that only ever sees the idle row cannot
            # show it. This is a scripted delay, not a device load (Rule 27).
            time.sleep(HOLD_SEC)
            if SCENARIO == "think":
                thinking_stream(c, LONG_THINKING, LONG_ANSWER)
            elif SCENARIO == "styled":
                text_stream(c, LONG_ANSWER + STYLED)
            else:
                text_stream(c, LONG_ANSWER)
        w.write(b"0\r\n\r\n")
        w.flush()
        mark(idx, "done")


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    sys.stderr.write("fakellm on %d scenario=%s\n" % (PORT, SCENARIO))
    srv.serve_forever()
