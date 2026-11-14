#!/usr/bin/env python3
"""google_cdp_server.py — persistent Google SERP gateway over headed Chromium CDP.

Runs ON search-bot. Talks to headed Chromium's CDP (ws://127.0.0.1:9222) using
the proven RPi5 pattern: /json/list discovery + Runtime.evaluate over a masked
WebSocket. Holds ONE debugger connection; navigates + extracts per query.
Pure stdlib.

Endpoint (same contract as the BiDi daemon, engine-agnostic for google_search.py):
  GET /search?q=<query>&n=<count>  -> {"status":..,"results":[{title,url,snippet}],"count":..}

Usage: python3 google_cdp_server.py [--port 9231]
"""
import base64
import json
import os
import socket
import struct
import sys
import threading
import time
import urllib.parse
import urllib.request

CDP_HOST, CDP_PORT = "127.0.0.1", 9222
SERVER_PORT = 9232          # distinct from BiDi daemon's 9231
RECV_TIMEOUT = 6
PAGE_LOAD_WAIT = 3.5
POLL_ROUNDS = 10
POLL_SLEEP = 1.5
MAX_RESULTS = 20
SNIPPET_CHARS = 200
CHALLENGE_CACHE_SEC = 900   # /sorry 挑战结果缓存时长（IP 级风控，15 分钟内必复现）
LOG = sys.stderr


def log(*a):
    LOG.write(time.strftime("[%H:%M:%S] ") + " ".join(str(x) for x in a) + "\n")
    LOG.flush()


class CDPConn:
    """One masked-WebSocket connection to a Chromium CDP page target."""

    def __init__(self, ws_path):
        self.s = socket.create_connection((CDP_HOST, CDP_PORT), timeout=RECV_TIMEOUT)
        self.s.settimeout(RECV_TIMEOUT)
        key = base64.b64encode(os.urandom(16)).decode()
        host = f"{CDP_HOST}:{CDP_PORT}"
        req = (f"GET {ws_path} HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\n"
               f"Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\n"
               f"Sec-WebSocket-Version: 13\r\n\r\n").encode()
        self.s.sendall(req)
        buf = b""
        while b"\r\n\r\n" not in buf:
            buf += self.s.recv(4096)
        if b" 101 " not in buf.split(b"\r\n", 1)[0]:
            raise ConnectionError("handshake %r" % (buf[:60],))
        self.seq = 0

    def close(self):
        try:
            self.s.close()
        except OSError:
            pass

    def _send(self, payload):
        plen = len(payload)
        if plen < 126:
            hdr = struct.pack(">BB", 0x81, 0x80 | plen)
        elif plen <= 0xFFFF:
            hdr = struct.pack(">BBH", 0x81, 0x80 | 126, plen)
        else:
            hdr = struct.pack(">BBQ", 0x81, 0x80 | 127, plen)
        mask = os.urandom(4)
        self.s.sendall(hdr + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(payload)))

    def _recv_frame(self):
        hdr = b""
        while len(hdr) < 2:
            c = self.s.recv(2 - len(hdr))
            if not c:
                raise ConnectionError("ws closed")
            hdr += c
        plen = hdr[1] & 0x7F
        masked = hdr[1] & 0x80
        if plen == 126:
            plen = struct.unpack(">H", self.s.recv(2))[0]
        elif plen == 127:
            plen = struct.unpack(">Q", self.s.recv(8))[0]
        msk = self.s.recv(4) if masked else b""
        body = b""
        while len(body) < plen:
            c = self.s.recv(plen - len(body))
            if not c:
                raise ConnectionError("ws closed mid")
            body += c
        if masked:
            body = bytes(b ^ msk[i % 4] for i, b in enumerate(body))
        return body

    def call(self, method, params, budget=20):
        self.seq += 1
        cid = self.seq
        self._send(json.dumps({"id": cid, "method": method, "params": params}).encode())
        self.s.settimeout(min(budget, 20))
        end = time.time() + budget
        while True:
            remaining = end - time.time()
            if remaining <= 0:
                raise TimeoutError("cdp no response for %s" % method)
            self.s.settimeout(min(remaining, 20))
            try:
                obj = json.loads(self._recv_frame().decode())
            except (ValueError, UnicodeDecodeError):
                continue
            if obj.get("id") == cid:
                return obj.get("result", {})


def get_page_target():
    """Find the first 'page' target on the CDP endpoint; return its ws url."""
    for _ in range(6):
        try:
            with urllib.request.urlopen(f"http://{CDP_HOST}:{CDP_PORT}/json/list",
                                        timeout=3) as r:
                pages = json.loads(r.read())
            for p in pages:
                if p.get("type") == "page":
                    u = p["webSocketDebuggerUrl"]
                    return u
        except Exception:
            pass
        time.sleep(1.5)
    return None


def cdp_eval(conn, expr, budget=20):
    """Runtime.evaluate; return the returned value (via returnByValue=True)."""
    r = conn.call("Runtime.evaluate",
                  {"expression": expr, "returnByValue": True}, budget=budget)
    return r.get("result", {}).get("value")


class Gateway:
    def __init__(self):
        self.lock = threading.Lock()
        self.conn = None
        self.last_challenge = 0.0   # 上次命中 /sorry 的时间戳（IP 级风控，短时间内必复现）

    def ensure_ready(self):
        if self.conn is None:
            self.try_connect()
        return self.conn

    def try_connect(self, tries=6):
        last = None
        for _ in range(tries):
            self.dispose()
            time.sleep(1.0)
            try:
                url = get_page_target()
                if not url:
                    last = "no page target"
                    time.sleep(2.0)
                    continue
                path = url.replace("ws://localhost:9222", "").replace(
                    f"ws://{CDP_HOST}:{CDP_PORT}", "")
                if not path.startswith("/"):
                    path = url
                conn = CDPConn(path)
                cdp_eval(conn, "1+1", budget=6)  # sanity
                self.conn = conn
                log("cdp ready path=%s" % path)
                return
            except Exception as e:
                last = repr(e)
                log("cdp connect fail:", last)
                time.sleep(2.0)
        raise RuntimeError("cannot connect cdp: %s" % last)

    def dispose(self):
        if self.conn:
            try:
                self.conn.close()
            except Exception:
                pass
            self.conn = None

    def search(self, query, count):
        with self.lock:
            try:
                self.ensure_ready()
            except Exception as e:
                return {"status": "unavailable", "message": repr(e), "results": [], "count": 0}
            # /sorry 是 IP 级风控：短时间内必复现，直接短路省一次完整导航（~10s）
            if time.time() - self.last_challenge < CHALLENGE_CACHE_SEC:
                return {"status": "empty", "results": [], "count": 0,
                        "message": "google served reCAPTCHA /sorry challenge for this IP (cached)"}
            conn = self.conn
            url = "https://www.google.com/search?%s" % urllib.parse.urlencode(
                {"q": query, "udm": "14", "hl": "en", "gl": "us", "num": str(count)})
            results, challenged = self._do_search(conn, url, count)
            for item in results:
                item["snippet"] = (item.get("snippet") or "")[:SNIPPET_CHARS]
            out = {"status": "ok" if results else "empty",
                   "results": results[:count],
                   "count": min(len(results), count)}
            if challenged and not results:
                out["message"] = "google served reCAPTCHA /sorry challenge for this IP"
            return out

    def _do_search(self, conn, url, count):
        results = []
        challenged = False
        last_e = None
        for attempt in range(1):
            try:
                try:
                    cdp_eval(conn, "location.href='%s';" % url, budget=4)
                except Exception:
                    pass
                time.sleep(PAGE_LOAD_WAIT)
                # quick challenge check before expensive polling
                try:
                    href = cdp_eval(conn, "location.href", budget=2)
                    if href and "sorry/index" in href:
                        challenged = True
                        self.last_challenge = time.time()   # 缓存挑战状态，短路后续请求
                        log("google challenge detected")
                        break
                except Exception as e:
                    log("href check err:", repr(e))
                js = extract_js(count)
                results = []
                for _ in range(POLL_ROUNDS):
                    try:
                        r = cdp_eval(conn, js, budget=4)
                    except Exception:
                        break
                    try:
                        results = json.loads(r) if isinstance(r, str) else (r or [])
                    except Exception:
                        results = r or []
                    if results:
                        break
                    time.sleep(POLL_SLEEP)
                if results or challenged:
                    break
            except Exception as e:
                last_e = e
                log("search attempt %d err:" % attempt, repr(e))
                self.dispose()
                return [], True
        return results, challenged


def extract_js(limit):
    return (
        "(function(){" "var out=[];var seen={};"
        "var h3s=Array.prototype.slice.call(document.querySelectorAll('h3'));"
        "for(var i=0;i<h3s.length&&out.length<%d;i++){"
        "var h=h3s[i];var title=h.textContent.trim();if(!title)continue;"
        "var a=h.closest('a');if(!a)continue;var href=a.href||'';"
        "if(!href||href.indexOf('google.com')>-1&&href.indexOf('/url?q=')==-1)continue;"
        "var real=href;var mm=real.match(/[?&]q=([^&]+)/);if(mm)real=decodeURIComponent(mm[1]);"
        "if(seen[real])continue;seen[real]=1;var snip='';var blk=a;"
        "for(var k=0;k<6&&blk;k++){blk=blk.parentElement;}"
        "if(blk){var ss=blk.querySelectorAll('[class^=VwiC3b]');"
        "if(ss.length)snip=ss[0].textContent.trim();}"
        "out.push({title:title,url:real,snippet:snip});" "}return out;})()" % limit)


def handle(gateway, conn):
    try:
        conn.settimeout(15)
        req = b""
        while b"\r\n\r\n" not in req:
            c = conn.recv(4096)
            if not c:
                return
            req += c
        head = req.split(b"\r\n", 1)[0].decode(errors="replace")
        parts = head.split()
        path = parts[1] if len(parts) > 1 else "/"
        # 轻量健康检查（watchdog 每 15s 调用；不做任何 Chrome 导航）
        if path.startswith("/health"):
            body = json.dumps({"status": "ok", "challenged": bool(
                time.time() - gateway.last_challenge < CHALLENGE_CACHE_SEC)}).encode()
            resp = (b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                    b"Content-Length: %d\r\nConnection: close\r\n\r\n" % len(body))
            conn.sendall(resp + body)
            return
        p = urllib.parse.urlparse(path)
        qs = urllib.parse.parse_qs(p.query)
        query = (qs.get("q") or [""])[0]
        count = min(int(qs.get("n", ["5"])[0]), MAX_RESULTS)
        t0 = time.time()
        out = gateway.search(query, count)
        out["elapsed_ms"] = int((time.time() - t0) * 1000)
        body = json.dumps(out, ensure_ascii=False).encode()
        resp = (b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                b"Content-Length: %d\r\nConnection: close\r\n\r\n" % len(body))
        conn.sendall(resp + body)
    except Exception as e:
        log("handle err:", repr(e))
    finally:
        try:
            conn.close()
        except OSError:
            pass


def main():
    port = SERVER_PORT
    if "--port" in sys.argv:
        port = int(sys.argv[sys.argv.index("--port") + 1])
    gateway = Gateway()
    try:
        gateway.ensure_ready()
    except Exception as e:
        log("warm-up failed:", repr(e))
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("127.0.0.1", port))
    srv.listen(5)
    log("cdp gateway listening on 127.0.0.1:%d" % port)
    while True:
        conn, _ = srv.accept()
        threading.Thread(target=handle, args=(gateway, conn), daemon=True).start()


if __name__ == "__main__":
    main()
