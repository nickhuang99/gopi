#!/usr/bin/env python3
"""Wikipedia search — 0-token websearch plugin script (W3).

Queries Wikipedia's MediaWiki API (en.wikipedia.org/w/api.php,
action=query&list=search) and returns a single slim JSON object. Wikipedia is
GFW-blocked from mainland China, so like github/google the fetch goes through
the search-bot VPS jump host when the direct route fails: try direct first
(works abroad, fastest), fall back to `ssh searchbox curl` (works behind the
GFW). All filtering/truncation happens inside this script so the agent only
pays tokens for the distilled result.

Usage:
  python3 wiki_search.py "retrieval augmented generation" [--count 5]

Output (single JSON object on stdout, always):
  {"status": "ok|empty|timeout|unavailable", "method": "wiki",
   "elapsed_ms": N,
   "results": [{"title": "...", "url": "https://en.wikipedia.org/wiki/...",
                "snippet": "..."}],
   "count": N}

status semantics (design doc gopi-websearch-plugin-design.md):
  ok          — >=1 result found
  empty       — query succeeded, no results
  timeout     — neither source answered within its timeout
  unavailable — any other failure (HTTP error, unparseable JSON, no ssh, ...)
"""
import argparse
import base64
import json
import re
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

# ---------------------------------------------------------------------------
# Constants — single source of truth, no magic numbers inline (Rule 1)
# ---------------------------------------------------------------------------
DEFAULT_COUNT = 5          # results returned when --count is omitted
MAX_COUNT = 20             # hard cap so the JSON payload stays slim (0-token)
SNIPPET_CHARS = 200        # snippet truncation length (contract limit)
DIRECT_TIMEOUT_SEC = 5          # direct API timeout; exceeded -> fall back to ssh
SSH_HOST = "searchbox"
SSH_OPTS = ["-o", "ConnectTimeout=8", "-o", "BatchMode=yes", "-T"]  # fast-fail + non-interactive
SSH_TIMEOUT_SEC = 40       # whole ssh round-trip cap (curl --max-time + handshake)
REMOTE_CURL_MAX_TIME = 20  # remote curl --max-time per fetch
UA = ("GoPiWebSearch/1.0 (wiki_search; MediaWiki API)")

API_BASE = "https://en.wikipedia.org/w/api.php"

# Remote bash script (base64-wrapped to avoid injection; runs on search-bot):
#   $1 = base64 HTTP URL. Prints @@HTTP:<code> then body on the next line.
REMOTE_SCRIPT = r"""set -u
url=$(printf '%s' "$1" | base64 -d 2>/dev/null)
a=$(mktemp) || exit 1
trap 'rm -f "$a"' EXIT
c=$(curl -sS -L --max-time __MT__ -A '__UA__' -o "$a" -w '%{http_code}' "$url")
echo "@@HTTP:$c"
cat "$a"
""".replace("__MT__", str(REMOTE_CURL_MAX_TIME)).replace("__UA__", UA)


def build_url(query, count):
    """Build the MediaWiki API search URL (JSON, snippet extraction on)."""
    params = {
        "action": "query",
        "list": "search",
        "srsearch": query,
        "srlimit": str(count),
        "srprop": "snippet",
        "format": "json",
        "origin": "*",  # CORS-friendly; harmless for server-side fetch
    }
    return API_BASE + "?" + "&".join(
        f"{k}={urllib.parse.quote(str(v))}" for k, v in params.items()
    )


def fetch_direct(url):
    """GET the MediaWiki API directly; raises on failure."""
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=DIRECT_TIMEOUT_SEC) as resp:
        return resp.read()


def fetch_via_ssh(url):
    """GET the MediaWiki API via the search-bot jump host; raises on failure.

    Returns (http_code, body). Raises on ssh failure or non-200 HTTP so the
    caller can degrade to unavailable.
    """
    b64 = base64.b64encode(url.encode()).decode()
    script_b64 = base64.b64encode(REMOTE_SCRIPT.encode()).decode()
    # Pipe the script into bash -s so $1 is set (plain `ssh host script arg`
    # concatenates and leaves $1 unbound under `set -u`).
    cmd = ["ssh"] + SSH_OPTS + [SSH_HOST,
           "echo %s | base64 -d | bash -s %s" % (script_b64, b64)]
    proc = subprocess.run(
        cmd, capture_output=True, text=True, timeout=SSH_TIMEOUT_SEC
    )
    if proc.returncode != 0:
        raise RuntimeError(f"ssh searchbox failed (rc={proc.returncode}): {proc.stderr.strip()[:200]}")
    out = proc.stdout
    marker = "@@HTTP:"
    idx = out.find(marker)
    if idx < 0:
        raise RuntimeError("ssh searchbox: no @@HTTP marker in output")
    code = int(out[idx + len(marker):idx + len(marker) + 3])
    body = out[idx + len(marker) + 3:].lstrip("\n")
    if code != 200:
        raise RuntimeError(f"wikipedia API HTTP {code} via searchbox")
    return body


def clean_text(text):
    """Strip HTML tags + unescape entities + collapse whitespace (snippet)."""
    if not text:
        return ""
    text = re.sub(r"<[^>]+>", "", text)
    import html as html_mod
    text = html_mod.unescape(text)
    return " ".join(text.strip().split())


def snippet(text):
    """Truncate snippet to SNIPPET_CHARS, appending '...' when cut."""
    text = clean_text(text)
    if len(text) > SNIPPET_CHARS:
        return text[:SNIPPET_CHARS] + "..."
    return text


def parse_results(json_bytes):
    """Parse the API JSON into the slim result list."""
    data = json.loads(json_bytes)
    items = ((data.get("query") or {}).get("search")) or []
    results = []
    for item in items:
        title = clean_text(item.get("title", ""))
        if not title:
            continue
        results.append({
            "title": title,
            "url": "https://en.wikipedia.org/wiki/" +
                   urllib.parse.quote(title.replace(" ", "_"), safe="/()"),
            "snippet": snippet(item.get("snippet", "")),
        })
    return results


def fetch_best(query, count):
    """Try direct, then search-bot. Returns (results, source). Raises on total failure."""
    url = build_url(query, count)
    last_err = None
    try:
        return parse_results(fetch_direct(url)), "direct"
    except (socket.timeout, TimeoutError, urllib.error.URLError,
            urllib.error.HTTPError, OSError, json.JSONDecodeError) as e:
        last_err = e
    try:
        return parse_results(fetch_via_ssh(url)), "searchbox"
    except (subprocess.TimeoutExpired, RuntimeError, OSError,
            json.JSONDecodeError) as e:
        raise RuntimeError(f"direct ({last_err!r}) and searchbox ({e!r}) both failed")


def main():
    ap = argparse.ArgumentParser(
        description="Search Wikipedia, return slim JSON (0-token plugin)."
    )
    ap.add_argument("query", help="search query, e.g. \"retrieval augmented generation\"")
    ap.add_argument("--count", type=int, default=DEFAULT_COUNT,
                    help=f"max results (default {DEFAULT_COUNT}, capped at {MAX_COUNT})")
    args = ap.parse_args()

    count = max(1, min(args.count, MAX_COUNT))
    start = time.monotonic()
    payload = {"status": "unavailable", "method": "wiki",
               "elapsed_ms": 0, "results": [], "count": 0}

    try:
        results, source = fetch_best(args.query, count)
        payload["status"] = "ok" if results else "empty"
        payload["results"] = results
        payload["count"] = len(results)
        payload["source"] = source
    except subprocess.TimeoutExpired as e:
        payload["status"] = "timeout"
        payload["message"] = f"wikipedia search timeout (ssh {SSH_TIMEOUT_SEC}s): {e}"
    except RuntimeError as e:
        payload["status"] = "unavailable"
        payload["message"] = f"wikipedia search failed: {e}"
    except Exception as e:  # noqa: BLE001 — plugin must always emit JSON
        payload["status"] = "unavailable"
        payload["message"] = f"wikipedia search failed: {e!r}"

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
