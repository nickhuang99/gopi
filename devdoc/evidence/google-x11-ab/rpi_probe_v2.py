#!/usr/bin/env python3
"""Probe v2 — same as rpi_arm_probe.py but WAITS for the X socket before
starting chromium (the v1 failure: 'X connection error received' because
`sleep 1` is not a wait). Also waits for /json/version to answer."""
import json, os, socket, subprocess, sys, time, urllib.parse, urllib.request

DISP, CDP_PORT = ":99", 9222
QUERY = sys.argv[1] if len(sys.argv) > 1 else "golang context deadline"

def wait_for(path, timeout=20):
    t0 = time.time()
    while time.time() - t0 < timeout:
        if os.path.exists(path):
            return True
        time.sleep(0.25)
    return False

def wait_cdp(timeout=40):
    t0 = time.time()
    while time.time() - t0 < timeout:
        try:
            s = socket.create_connection(("127.0.0.1", CDP_PORT), 2)
            s.sendall(b"GET /json/version HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
            data = s.recv(4096); s.close()
            if b"Browser" in data:
                return True
        except OSError:
            pass
        time.sleep(0.5)
    return False

os.system("pkill -9 -f 'Xvfb :99' 2>/dev/null")
os.system("pkill -9 -f 'remote-debugging-port=9222' 2>/dev/null")
time.sleep(2)
for p in (X11_LOCK, X11_SOCK):
    try: os.remove(p)
    except OSError: pass

subprocess.Popen(["Xvfb", DISP, "-screen", "0", "1280x1024x24"],
                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
xsock_ok = wait_for(X11_SOCK, 20)
env = os.environ.copy(); env["DISPLAY"] = DISP
prof = PROFILE_DIR
subprocess.Popen(["chromium", "--disable-blink-features=AutomationControlled",
                  "--no-first-run", "--no-sandbox", "--user-data-dir=" + prof,
                  "--remote-debugging-port=%d" % CDP_PORT, "about:blank"],
                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=env)
cdp_ok = wait_cdp(45)
print(json.dumps({"xsock_ok": xsock_ok, "cdp_ok": cdp_ok}), flush=True)
if not cdp_ok:
    sys.exit(3)

def http_get(host, port, path):
    s = socket.create_connection((host, port), 5)
    s.sendall(("GET %s HTTP/1.1\r\nHost: %s\r\n\r\n" % (path, host)).encode())
    buf = b""
    s.settimeout(5)
    try:
        for _ in range(RESP_READ_CAP):
            c = s.recv(4096)
            if not c: break
            buf += c
    except OSError: pass
    s.close()
    return buf.decode(errors="ignore").split("\r\n\r\n", 1)[-1]

tabs = json.loads(http_get("127.0.0.1", CDP_PORT, "/json"))
ws_url = next(t["webSocketDebuggerUrl"] for t in tabs if t.get("type") == "page")
target_id = ws_url.rsplit("/", 1)[-1]

# Masked websocket handshake (the proven rpi driver, memory #189)
key = "dGhlIHNhbXBsZSBub25jZQ=="
host, port = "127.0.0.1", CDP_PORT
path = "/devtools/page/" + target_id
s = socket.create_connection((host, port), 10)
req = ("GET %s HTTP/1.1\r\nHost: %s:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
       "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n" % (path, host, port, key))
s.sendall(req.encode())
hs = s.recv(4096)
if b"101" not in hs.split(b"\r\n")[0]:
    print(json.dumps({"error": "ws handshake failed", "hs": hs.decode(errors="ignore")[:200]})); sys.exit(4)

def mask_frame(payload):
    b = payload.encode()
    hdr = bytearray([0x81])
    n = len(b)
    if n < 126: hdr.append(0x80 | n)
    elif n < 65536: hdr.append(0x80 | 126); hdr += struct.pack(">H", n)
    else: hdr.append(0x80 | 127); hdr += struct.pack(">Q", n)
    m = os.urandom(4); hdr += m
    return bytes(hdr) + bytes(x ^ m[i % 4] for i, x in enumerate(b))

import struct

# The X11 socket directory is the ONE /tmp path this repository keeps
# (internal/shot/x11.go keeps the same one in Go): a unix socket there is
# created by the X server, never written by us. It comes from the shared
# helper so no probe spells it (Rule 14: one definition, every consumer).
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                "..", "..", "..", "scripts", "lib"))
from tmp_ban import make_scratch_dir, x11_base_dir, x11_socket_path  # noqa: E402

RESP_READ_CAP = 4096  # bounds the CDP /json read (64 KiB per read is ample)
X11_SOCK = x11_socket_path(":99")          # <socket dir>/X99
X11_LOCK = os.path.join(x11_base_dir(), ".X99-lock")
# Probe scratch (not the X socket): off `/`, GC'd, from the helper.
PROFILE_DIR = make_scratch_dir("rpi-profile-v2")

mid = [0]
def send(method, params=None):
    mid[0] += 1
    s.sendall(mask_frame(json.dumps({"id": mid[0], "method": method, "params": params or {}})))
    return mid[0]

def read_until(cmd_id, timeout=45):
    s.settimeout(timeout)
    buf = b""
    t0 = time.time()
    while time.time() - t0 < timeout:
        try: chunk = s.recv(65536)
        except socket.timeout: break
        if not chunk: break
        buf += chunk
        try:
            for msg in decode_frames(buf):
                if msg.get("id") == cmd_id:
                    return msg
        except Exception: pass
    return None

def decode_frames(buf):
    out, i = [], 0
    while i < len(buf):
        b1 = buf[i]
        if b1 & 0x0F == 8: break
        plen = buf[i+1] & 0x7F; i += 2
        if plen == 126: plen = struct.unpack(">H", buf[i:i+2])[0]; i += 2
        elif plen == 127: plen = struct.unpack(">Q", buf[i:i+8])[0]; i += 8
        if i + plen > len(buf): break
        payload = buf[i:i+plen]; i += plen
        if (buf[i-plen-1] & 0x80) if False else True:
            pass
        try: out.append(json.loads(payload.decode(errors="ignore")))
        except Exception: pass
    return out

send("Page.enable"); time.sleep(0.5)
url = "https://www.google.com/search?" + urllib.parse.urlencode({"q": QUERY, "num": 8})
cid = send("Page.navigate", {"url": url})
time.sleep(10)
res = send("Runtime.evaluate", {"expression":
    "JSON.stringify({href:location.href,title:document.title,"
    "body:(document.body?document.body.innerText:'').slice(0,600),"
    "h3:[...document.querySelectorAll('h3')].map(e=>e.innerText).slice(0,8)})",
    "returnByValue": True})
msg = read_until(res, 40)
out = {"query": QUERY, "nav_url": url}
try:
    val = json.loads(msg["result"]["result"]["value"])
    out.update({"landed_href": val["href"], "title": val["title"],
                "body_head": val["body"], "h3_titles": val["h3"],
                "sorry": 1 if "/sorry" in val["href"] else 0,
                "enablejs": 1 if "enablejs" in val["body"] else 0})
except Exception as e:
    out["error"] = repr(e); out["raw"] = json.dumps(msg)[:500]
print(json.dumps(out, indent=1))
