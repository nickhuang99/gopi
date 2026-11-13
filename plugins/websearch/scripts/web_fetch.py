#!/usr/bin/env python3
"""web_fetch — fetch a URL and return cleaned full text (0-token websearch
plugin script, the fetch side of the search/fetch split).

Search ≠ Read: web_search returns snippets (metadata only); deep research and
source verification need the actual page text. This script fetches ONE URL
and returns the cleaned article/body text, truncated to a token-budget cap.

Path (mirrors google_search.py / github_search.py):
  - URL validated locally (http/https only).
  - Body fetched on the search-bot VPS jump host (GFW'd local host cannot
    reach many overseas sites): ssh searchbot 'curl -sL --max-time N URL'.
    The URL is base64-wrapped so no shell injection can pass through.
  - HTML cleaned: script/style/nav/header/footer/noscript/iframe dropped,
    block tags → newlines, entities unescaped, whitespace collapsed.
  - Truncated from the TAIL at --max-chars (default 8000) with a marker, so
    the JSON stays slim (0-token discipline).

Output (stdout, single-line JSON, always):
  {"status":"ok|error|timeout|unavailable","url":"...","chars":N,
   "truncated":bool,"content":"...","elapsed_ms":N,"message":"..."}

status semantics:
  ok          — fetched + cleaned text returned (possibly truncated)
  error       — bad URL scheme, HTTP >= 400, binary/non-text body
  timeout     — remote curl timed out (--max-time exceeded)
  unavailable — ssh/jump-host failure, connection refused/DNS, script failure

Usage:
  python3 web_fetch.py "<url>" [--max-chars N] [--max-time N]
"""
import argparse
import base64
import html.parser
import json
import os
import re
import signal
import subprocess
import sys
import time
import urllib.parse

# ---------------------------------------------------------------------------
# Constants — single source of truth, no magic numbers inline (Rule 1)
# ---------------------------------------------------------------------------
DEFAULT_MAX_CHARS = 8000      # cleaned-text budget (0-token discipline)
MIN_MAX_CHARS = 200           # floor so a tiny budget still yields something
MAX_MAX_CHARS = 100000        # hard cap — protects the LLM context window
DEFAULT_MAX_TIME = 30         # remote curl --max-time (seconds)
MAX_MAX_TIME = 120            # hard cap on the remote fetch
SSH_HOST = os.environ.get("SSH_HOST", "searchbot")
# Direct-fetch domains: hosts reachable from the LOCAL network (CN sites).
# Fetching these via the US jump host distorts geo-sensitive content
# (pricing pages show USD instead of CNY, CN-only variants are hidden).
# Local direct curl keeps the caller's real IP → geo-correct responses.
DIRECT_DOMAINS = tuple(
    d.strip().lower()
    for d in os.environ.get("WEB_FETCH_DIRECT_DOMAINS", "bigmodel.cn,z.ai,open.bigmodel.cn,docs.bigmodel.cn").split(",")
    if d.strip()
)
# Hard gate: localhost/private ranges must NEVER go direct — SSRF protection
# (a "CN-looking" host that resolves locally could reach internal services).
_DIRECT_BLOCK_RE = re.compile(
    r"^(localhost|127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|169\.254\.|\[::1\])", re.I)
SSH_OPTS = ["-o", "ConnectTimeout=8", "-o", "BatchMode=yes", "-T"]  # fast-fail + non-interactive
UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
TRUNCATION_MARKER = "…[truncated — tail omitted]"
MAX_HTTP_CODE = 399           # anything above is an HTTP error page
CURL_RC_TIMEOUT = 28          # curl exit code for --max-time expiry
# HTML elements whose text never belongs in a readable article are stripped
# by _DROP_PAIRED_RE / _DROP_VOID_RE in clean_html (regex — robust against
# unclosed tags in the wild).
# Block-level elements: their text gets a newline separation.
BLOCK_TAGS = frozenset({
    "p", "div", "br", "li", "tr", "section", "article", "blockquote",
    "pre", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "table",
    "hr", "figure", "figcaption", "main", "details", "summary",
})

# Drop-tag stripping happens by regex BEFORE parsing (paired sections then
# void elements). Parser-side depth tracking is fragile: HTML in the wild has
# unclosed tags (e.g. <input> has no </input>), which would leave the drop
# depth stuck and swallow the whole page after the first unclosed tag.
_DROP_PAIRED_RE = re.compile(
    r"<(script|style|noscript|iframe|template|svg|canvas|nav|header|footer|"
    r"form|aside|dialog|menu|select|option|textarea|button)\b[^>]*>.*?</\1\s*>",
    re.S | re.I)
_DROP_VOID_RE = re.compile(
    r"<(input|br|img|hr|meta|link|source|area|base|col|embed|param|track|wbr)\b[^>]*/?>",
    re.I)

# Remote bash script (base64-wrapped to avoid injection; runs on search-bot):
#   $1 = base64 HTTP(S) URL, $2 = base64 --max-time value.
# Prints "@@HTTP:<code>|<content-type> RC:<curl-rc>" then the raw body.
# One single GET supplies code + content-type (no extra HEAD round-trip).
REMOTE_SCRIPT = r"""set -u
url=$(printf '%s' "$1" | base64 -d 2>/dev/null)
mt=$(printf '%s' "$2" | base64 -d 2>/dev/null)
[ -n "$mt" ] || mt=30
a=$(mktemp) || exit 1
trap 'rm -f "$a"' EXIT
meta=$(curl -sS -L --max-time "$mt" -A '__UA__' -o "$a" -w '%{http_code}|%{content_type}' "$url"); rc=$?
echo "@@HTTP:$meta RC:$rc"
cat "$a"
""".replace("__UA__", UA)


class _TextExtractor(html.parser.HTMLParser):
    """HTML → readable text: block tags become newlines, entities unescaped.
    Drop tags were already stripped by regex (see clean_html)."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self._parts = []

    def handle_starttag(self, tag, attrs):
        if tag in BLOCK_TAGS:
            self._newline()

    def handle_startendtag(self, tag, attrs):
        if tag in BLOCK_TAGS:
            self._newline()

    def handle_endtag(self, tag):
        if tag in BLOCK_TAGS:
            self._newline()

    def handle_data(self, data):
        self._parts.append(data)

    def _newline(self):
        if self._parts and not self._parts[-1].endswith("\n"):
            self._parts.append("\n")

    def text(self):
        raw = "".join(self._parts)
        # Collapse runs of whitespace within a line, drop blank lines.
        lines = [re.sub(r"[ \t\u00a0]+", " ", ln).strip() for ln in raw.splitlines()]
        lines = [ln for ln in lines if ln]
        return "\n".join(lines).strip()


def clean_html(body):
    """Strip chrome/boilerplate tags and return readable plain text."""
    body = _DROP_PAIRED_RE.sub(" ", body)
    body = _DROP_VOID_RE.sub(" ", body)
    parser = _TextExtractor()
    try:
        parser.feed(body)
        parser.close()
    except Exception:
        # Malformed HTML must never crash the script — regex fallback.
        return re.sub(r"<[^>]+>", " ", body)
    return parser.text()


def looks_binary(body):
    """NUL bytes are a strong signal of a non-text (pdf/image/zip) body.
    Accepts bytes (checked before decoding) or str."""
    sample = body[:8192]
    if isinstance(sample, bytes):
        return b"\x00" in sample
    return "\x00" in sample


def validate_url(url):
    """Return (ok, error_msg). Only http/https allowed."""
    if not url or not url.strip():
        return False, "empty url"
    u = urllib.parse.urlparse(url.strip())
    if u.scheme not in ("http", "https"):
        return False, f"unsupported scheme {u.scheme!r} (only http/https)"
    if not u.netloc:
        return False, "missing host"
    return True, ""


def _direct_fetch(url, max_time):
    """Fetch from the LOCAL network (CN sites — geo-correct, no jump host).
    Returns (status, body, msg) — same contract as _ssh_fetch."""
    cmd = ["curl", "-sS", "-L", "--max-time", str(max_time), "-A", UA, url]
    try:
        proc = subprocess.run(cmd, capture_output=True, timeout=max_time + 10)
    except subprocess.TimeoutExpired:
        return "timeout", "", f"direct curl timed out after {max_time}s"
    if proc.returncode == CURL_RC_TIMEOUT:
        return "timeout", "", f"direct curl timed out after {max_time}s"
    if proc.returncode != 0:
        return "unavailable", "", f"direct curl failed (rc={proc.returncode})"
    body = proc.stdout
    if looks_binary(body):
        return "error", "", "body is not text (binary content)"
    return "ok", body, "text/html; direct"


def _pick_route(url):
    """Route decision: direct (local IP, geo-correct) for DIRECT_DOMAINS hosts,
    ssh jump host for everything else. Private/localhost always ssh-side so the
    direct path can never reach internal services (SSRF hard gate)."""
    netloc = urllib.parse.urlparse(url).netloc.lower()
    # Gate on the raw netloc (covers [::1]:8080 — splitting on ":" first would
    # mangle bracketed IPv6 into "[" and slip past the regex).
    if _DIRECT_BLOCK_RE.match(netloc):
        return "ssh"
    if netloc.startswith("["):  # bracketed IPv6 literal
        host = netloc[1:netloc.index("]")]
    else:
        host = netloc.split(":")[0]
    if any(host == d or host.endswith("." + d) for d in DIRECT_DOMAINS):
        return "direct"
    return "ssh"


def _ssh_fetch(url, max_time):
    """Run the remote curl on search-bot via ssh. Returns (status, body, msg)."""
    url_b64 = base64.b64encode(url.encode("utf-8")).decode()
    mt_b64 = base64.b64encode(str(max_time).encode("utf-8")).decode()
    remote_b64 = base64.b64encode(REMOTE_SCRIPT.encode("utf-8")).decode()
    # Whole round-trip cap: remote curl (max_time) + ssh handshake (~2-8s).
    ssh_timeout = max_time + 15
    cmd = ["ssh"] + SSH_OPTS + [SSH_HOST,
           "echo %s | base64 -d | bash -s %s %s" % (remote_b64, url_b64, mt_b64)]
    try:
        proc = subprocess.run(cmd, capture_output=True, timeout=ssh_timeout)
    except subprocess.TimeoutExpired:
        return "timeout", "", f"ssh to {SSH_HOST} exceeded {ssh_timeout}s (remote curl max-time {max_time}s)"
    if proc.returncode != 0:
        return "unavailable", "", f"ssh to {SSH_HOST} failed (rc={proc.returncode})"
    # Capture as BYTES: binary bodies (pdf/image) would crash a text=True
    # decode — the script must never raise. The marker line is ASCII.
    raw = proc.stdout
    # Split off ONLY the "@@HTTP:..." marker line; everything after the first
    # \n is body (single-line/minified HTML included — find+slice keeps it
    # intact; the old lines[2] indexing silently dropped such bodies).
    newline_idx = raw.find(b"\n")
    if newline_idx < 0:
        return "unavailable", "", f"unexpected payload from {SSH_HOST}: no marker line"
    marker = raw[:newline_idx].decode("utf-8", errors="replace").strip()
    body = raw[newline_idx + 1:]
    m = re.match(r"^@@HTTP:(.+)$", marker)
    if not m:
        return "unavailable", "", f"unexpected payload from {SSH_HOST}: {marker}"
    meta_rc = m.group(1)
    if " RC:" not in meta_rc:
        return "unavailable", "", f"unexpected payload from {SSH_HOST}: {marker}"
    meta, rc = meta_rc.rsplit(" RC:", 1)
    code, _, content_type = meta.partition("|")
    if rc == str(CURL_RC_TIMEOUT):
        return "timeout", "", f"remote curl timed out after {max_time}s"
    if code == "000":
        return "unavailable", "", f"connection failed (curl rc={rc}, http 000)"
    code_i = int(code) if code.isdigit() else 0
    if code_i > MAX_HTTP_CODE:
        return "error", "", f"HTTP {code} (content-type: {content_type or 'unknown'})"
    return "ok", body, content_type


def main():
    # --cov-mark-route is a TEST hook, not a fetch: with it, main() resolves
    # the geo route for each given URL and prints "<url>|<route>" per probe,
    # then exits — zero network, zero subprocess. The contract suite asserts
    # the direct/ssh + SSRF routing on REAL _pick_route logic, not a stub.
    if "--cov-mark-route" in sys.argv:
        i = sys.argv.index("--cov-mark-route")
        urls = sys.argv[i + 1:]
        if "--" in urls:
            urls = urls[:urls.index("--")]
        for u in urls:
            try:
                print("%s|%s" % (u, _pick_route(u)))
            except Exception as e:  # never crash the probe
                print("%s|ERROR:%s" % (u, e))
        return

    signal.signal(signal.SIGPIPE, signal.SIG_DFL)  # | head 友好退出
    ap = argparse.ArgumentParser(
        description="Fetch a URL and return cleaned full text (search/fetch split — the read side)."
    )
    ap.add_argument("url", help="http(s) URL to fetch, e.g. a web_search result URL")
    ap.add_argument("--max-chars", type=int, default=DEFAULT_MAX_CHARS,
                    help=f"max cleaned-text chars (default {DEFAULT_MAX_CHARS})")
    ap.add_argument("--max-time", type=int, default=DEFAULT_MAX_TIME,
                    help=f"remote curl --max-time seconds (default {DEFAULT_MAX_TIME})")
    args = ap.parse_args()

    start = time.monotonic()
    max_chars = max(MIN_MAX_CHARS, min(args.max_chars, MAX_MAX_CHARS))
    max_time = max(1, min(args.max_time, MAX_MAX_TIME))
    url = args.url.strip()

    payload = {"status": "unavailable", "url": url, "chars": 0,
               "truncated": False, "content": "", "elapsed_ms": 0}

    ok, err = validate_url(url)
    if not ok:
        payload["status"] = "error"
        payload["message"] = err
    else:
        route = _pick_route(url)
        if route == "direct":
            status, body, msg = _direct_fetch(url, max_time)
            if status != "ok":
                # direct path failed → never silently fall back for geo reasons;
                # report the failure so the LLM knows the content is absent.
                payload["status"] = status
                payload["message"] = msg + " (direct route)"
                payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
                print(json.dumps(payload, ensure_ascii=False))
                return
        else:
            status, body, msg = _ssh_fetch(url, max_time)
        if status == "ok":
            if looks_binary(body):
                payload["status"] = "error"
                payload["message"] = "body is not text (binary content)"
            else:
                if isinstance(body, bytes):
                    body = body.decode("utf-8", errors="replace")
                text = clean_html(body)
                truncated = len(text) > max_chars
                if truncated:
                    text = text[:max_chars] + TRUNCATION_MARKER
                payload["status"] = "ok"
                payload["chars"] = len(text)
                payload["truncated"] = truncated
                payload["content"] = text
                if msg:
                    payload["message"] = f"content-type: {msg}"
        else:
            payload["status"] = status
            payload["message"] = msg

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
