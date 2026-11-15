#!/usr/bin/env python3
"""rpi_arm_probe.py — residential-egress arm probe, run ON the RPi5.

RPi5 has chromium + Xvfb but NO xdotool, so the X11-input technique used on
search-bot is not available here; this probe uses the RPi5's own proven driver
(rpi_google_search.py's masked-WebSocket CDP, memory #189) and reports the SAME
per-cell signal the search-bot arms use: landed URL, title, challenge markers,
h3 titles, body head.

Landed URL + body head are what carry the cited-IP evidence: a /sorry page names
the exit IP in its body, so this single reading answers "which IP did Google see
and what did it decide".

Usage: python3 rpi_arm_probe.py "query"
"""
import json
import os
import random
import socket
import struct
import subprocess
import sys
import time
import urllib.parse
import urllib.request

# The X11 socket directory is the ONE /tmp path this repository keeps
# (internal/shot/x11.go keeps the same one in Go): a unix socket there is
# created by the X server, never written by us. It comes from the shared
# helper so no probe spells it (Rule 14: one definition, every consumer).
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                "..", "..", "..", "scripts", "lib"))
from tmp_ban import x11_base_dir, x11_socket_path  # noqa: E402

RESP_READ_CAP = 4096  # bounds the CDP /json read (64 KiB per read is ample)
X11_SOCK = x11_socket_path(":99")          # <socket dir>/X99
X11_LOCK = os.path.join(x11_base_dir(), ".X99-lock")


DISP = ":99"
CDP_PORT = 9222


def send_frame(ws, payload):
    pl = payload.encode() if isinstance(payload, str) else payload
    mk = bytes([random.randint(0, 255) for _ in range(4)])
    if len(pl) < 126:
        hdr = struct.pack(">BB", 0x81, 0x80 | len(pl))
    elif len(pl) < 65536:
        hdr = struct.pack(">BBH", 0x81, 0x80 | 126, len(pl))
    else:
        hdr = struct.pack(">BBQ", 0x81, 0x80 | 127, len(pl))
    masked = bytes([pl[i] ^ mk[i % 4] for i in range(len(pl))])
    ws.sendall(hdr + mk + masked)


def recv_frames(ws, want_id, timeout=20):
    data = b""
    end = time.time() + timeout
    while time.time() < end:
        try:
            ws.settimeout(5)
            data += ws.recv(65536)
        except socket.timeout:
            continue
        while len(data) >= 2:
            plen = data[1] & 0x7F
            off = 2
            if plen == 126 and len(data) >= 4:
                plen = struct.unpack(">H", data[2:4])[0]; off = 4
            elif plen == 127 and len(data) >= 10:
                plen = struct.unpack(">Q", data[2:10])[0]; off = 10
            if len(data) < off + plen:
                break
            payload = data[off:off + plen]; data = data[off + plen:]
            try:
                obj = json.loads(payload.decode())
            except ValueError:
                continue
            if obj.get("id") == want_id:
                return obj.get("result", {})
    return {}


def main():
    query = sys.argv[1] if len(sys.argv) > 1 else "golang context deadline"
    out = {"query": query, "display": DISP, "cdp_port": CDP_PORT}
    xvfb = chrome = None
    try:
        os.system("pkill -9 -f 'chromium --disable-blink' 2>/dev/null")
        os.system("pkill -9 -f 'Xvfb :99' 2>/dev/null")
        time.sleep(1)
        for p in (X11_LOCK,):
            try:
                os.remove(p)
            except OSError:
                pass
        xvfb = subprocess.Popen(["Xvfb", DISP, "-screen", "0", "1280x1024x24"],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        # WAIT for the X socket: a bare sleep is not a wait (the known
        # "X connection error received" failure that killed the first attempts).
        _t0 = time.time()
        while not os.path.exists(X11_SOCK) and time.time() - _t0 < 25:
            time.sleep(0.25)
        time.sleep(1)
        env = os.environ.copy(); env["DISPLAY"] = DISP
        chrome = subprocess.Popen(
            ["chromium", "--disable-blink-features=AutomationControlled", "--no-first-run",
             "--remote-debugging-port=%d" % CDP_PORT, "about:blank"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=env)
        _t0 = time.time()
        while time.time() - _t0 < 45:
            try:
                _p = socket.create_connection(("127.0.0.1", CDP_PORT), 2)
                _p.sendall(b"GET /json/version HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
                if b"Browser" in _p.recv(4096):
                    _p.close(); break
                _p.close()
            except OSError:
                pass
            time.sleep(0.5)

        s = socket.socket(); s.settimeout(5); s.connect(("127.0.0.1", CDP_PORT))
        s.sendall(b"GET /json HTTP/1.1\r\nHost: 127.0.0.1:9222\r\n\r\n")
        resp = b""
        try:
            for _ in range(RESP_READ_CAP):
                resp += s.recv(4096)
        except OSError:
            pass
        s.close()
        tabs = json.loads(resp.decode(errors="ignore").split("\r\n\r\n", 1)[-1])
        ws_url = next(t["webSocketDebuggerUrl"] for t in tabs if t.get("type") == "page")
        port_p, path = ws_url.replace("ws://127.0.0.1:", "").split("/", 1)
        ws = socket.socket(); ws.settimeout(30)
        ws.connect(("127.0.0.1", int(port_p)))
        ws.sendall(("GET /%s HTTP/1.1\r\nHost: 127.0.0.1:%s\r\nUpgrade: websocket\r\n"
                    "Connection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"
                    "Sec-WebSocket-Version: 13\r\n\r\n" % (path, port_p)).encode())
        upg = b""
        while b"\r\n\r\n" not in upg:
            upg += ws.recv(1)
        out["ws_upgrade_ok"] = b" 101 " in upg.split(b"\r\n", 1)[0]

        # warm-up leg: the homepage first (same shape as the search-bot arms)
        send_frame(ws, json.dumps({"id": 1, "method": "Page.navigate",
                                   "params": {"url": "https://www.google.com/"}}))
        time.sleep(5)
        send_frame(ws, json.dumps({"id": 2, "method": "Runtime.evaluate",
                                   "params": {"expression": "document.title",
                                              "returnByValue": True}}))
        out["warmup_title"] = recv_frames(ws, 2).get("result", {}).get("value", "")

        url = "https://www.google.com/search?" + urllib.parse.urlencode(
            {"q": query, "num": 8})
        out["nav_url"] = url
        send_frame(ws, json.dumps({"id": 3, "method": "Page.navigate", "params": {"url": url}}))
        time.sleep(6)
        js = ("JSON.stringify({href:location.href,title:document.title,"
              "body:(document.body?document.body.innerText:'').slice(0,900),"
              "h3:Array.prototype.slice.call(document.querySelectorAll('h3'))"
              ".slice(0,10).map(function(h){return h.textContent.trim()})})")
        send_frame(ws, json.dumps({"id": 4, "method": "Runtime.evaluate",
                                   "params": {"expression": js, "returnByValue": True}}))
        val = recv_frames(ws, 4).get("result", {}).get("value") or "{}"
        d = json.loads(val)
        low = (d.get("body") or "").lower()
        out.update({
            "landed_href": d.get("href", ""),
            "title": d.get("title", ""),
            "body_head": d.get("body", ""),
            "h3_titles": d.get("h3", []),
            "sorry": int("sorry/index" in (d.get("href") or "") or "unusual traffic" in low
                         or "not a robot" in low),
            "enablejs": int("enablejs" in (d.get("href") or "") or "enablejs" in low),
        })
    except Exception as e:
        out["error"] = repr(e)
    finally:
        for p in (chrome, xvfb):
            if p and p.poll() is None:
                try:
                    p.terminate(); p.wait(timeout=5)
                except Exception:
                    p.kill()
    print(json.dumps(out, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
