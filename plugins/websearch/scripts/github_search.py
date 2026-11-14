#!/usr/bin/env python3
"""GitHub search (repos / code / issues) — 0-token websearch plugin script.

Queries GitHub via the search-bot VPS jump host (search-bot has a direct
overseas route to api.github.com / github.com, unlike the GFW'd local host).
Two sources, primary then fallback:

  1) GitHub REST Search API: api.github.com/search/...
     - repositories / issues work UNAUTHENTICATED (rate limited ~10/min, 60/h).
     - code search REQUIRES an auth token (GITHUB_TOKEN) — returns 401 without one.
  2) GitHub web search fallback: github.com/search?q=..&type=repositories|issues
     server-rendered JSON (embeddedData) parsed here. Code web search requires
     login, so code with no token degrades to an honest status=unavailable.

Output (single JSON object on stdout, always):
  {"status":"ok|empty|unavailable","method":"github",
   "elapsed_ms":N,
   "results":[{"title":"..","url":"..","snippet":".."}],
   "count":N}

status semantics (design doc gopi-websearch-plugin-design.md):
  ok          — >=1 result found
  empty       — source answered, no results
  unavailable — any failure (401 code-no-token, rate limit unresolvable, network, ...)

Usage:
  python3 github_search.py "<query>" [--count N] \
      [--type repositories|code|issues]   (default repositories)
"""
import argparse
import base64
import html as html_mod
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
DEFAULT_COUNT = 5        # results returned when --count is omitted
MAX_COUNT = 20           # hard cap so the JSON payload stays slim (0-token)
SNIPPET_CHARS = 200      # snippet truncation length (contract limit)
SSH_HOST = os.environ.get("SSH_HOST", "searchbox")
SSH_OPTS = ["-o", "ConnectTimeout=8", "-o", "BatchMode=yes", "-T"]  # fast-fail + non-interactive
SSH_TIMEOUT_SEC = 40     # whole ssh round-trip cap (curl --max-time 20 + handshake)
REMOTE_CURL_MAX_TIME = 20  # remote curl --max-time per fetch
UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")

API_BASE = "https://api.github.com/search"
WEB_BASE = "https://github.com/search"
# Web search type= parameter per kind (issues uses "issues", not "issue").
WEB_TYPE = {"repositories": "repositories", "code": "code", "issues": "issues"}

# Remote bash script (base64-wrapped to avoid injection; runs on search-bot):
#   $1 = base64 HTTP URL, $2 = type marker (api|web). Prints:
#   @@HTTP:<code> then body on the next line.
REMOTE_SCRIPT = r"""set -u
url=$(printf '%s' "$1" | base64 -d 2>/dev/null)
tok=$(printf '%s' "$2" | base64 -d 2>/dev/null)
a=$(mktemp) || exit 1
trap 'rm -f "$a"' EXIT
if [ -n "$tok" ]; then
  c=$(curl -sS -L --max-time __MT__ -A '__UA__' -H "Authorization: Bearer $tok" -o "$a" -w '%{http_code}' "$url")
else
  c=$(curl -sS -L --max-time __MT__ -A '__UA__' -o "$a" -w '%{http_code}' "$url")
fi
echo "@@HTTP:$c"
cat "$a"
""".replace("__MT__", str(REMOTE_CURL_MAX_TIME)).replace("__UA__", UA)


def strip_tags(s):
    """Strip HTML tags + unescape entities + collapse whitespace."""
    s = re.sub(r"<[^>]+>", "", s)
    return " ".join(html_mod.unescape(s).split())


def truncate(s, n=SNIPPET_CHARS):
    """Truncate to AT MOST n chars (ellipsis included) so the contract
    'snippet <= 200 chars' always holds."""
    s = s.strip()
    if len(s) > n:
        return s[:max(0, n - 3)] + "..."
    return s


# Optional GitHub token: needed ONLY for --type code API search. Not required
# for repositories/issues. Read once at import (env override, no source of
# truth duplication — absent = the web/rate-limit paths still work).
GITHUB_TOKEN = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN") or ""


def _ssh_fetch(url):
    """Run the remote curl on search-bot via ssh. Returns (status, body, msg)."""
    url_b64 = base64.b64encode(url.encode("utf-8")).decode()
    tok_b64 = base64.b64encode(GITHUB_TOKEN.encode("utf-8")).decode()
    remote_b64 = base64.b64encode(REMOTE_SCRIPT.encode("utf-8")).decode()
    cmd = ["ssh"] + SSH_OPTS + [SSH_HOST,
           "echo %s | base64 -d | bash -s %s %s" % (remote_b64, url_b64, tok_b64)]
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True,
                              timeout=SSH_TIMEOUT_SEC)
    except subprocess.TimeoutExpired:
        return "unavailable", "", f"ssh to {SSH_HOST} timed out after {SSH_TIMEOUT_SEC}s"
    if proc.returncode != 0:
        return "unavailable", "", f"ssh to {SSH_HOST} failed (rc={proc.returncode})"
    stdout = proc.stdout
    lines = stdout.split("\n", 1)
    marker = lines[0].strip()
    body = lines[1] if len(lines) > 1 else ""
    if not marker.startswith("@@HTTP:"):
        return "unavailable", "", f"unexpected payload from {SSH_HOST}: {marker}"
    code = marker.split(":", 1)[1].strip()
    return "ok" if code != "000" else "timeout", body, code


# ---------------------------------------------------------------------------
# API parsing
# ---------------------------------------------------------------------------
def parse_api_repositories(body):
    """REST /search/repositories -> unified results."""
    results = []
    for it in (json.loads(body).get("items") or []):
        name = it.get("full_name") or ""
        desc = it.get("description") or ""
        lang = it.get("language") or ""
        title = name + (f" · {lang}" if lang else "")
        url = it.get("html_url") or f"https://github.com/{name}"
        snip = truncate(desc) if desc else f"{it.get('stargazers_count','?')} stars"
        results.append({"title": title, "url": url, "snippet": snip})
    return results


def parse_api_issues(body):
    """REST /search/issues -> unified results."""
    results = []
    for it in (json.loads(body).get("items") or []):
        # items API returns the repository object EMPTY; repository_url
        # (https://api.github.com/repos/OWNER/NAME) carries the identity.
        full = ""
        rurl = it.get("repository_url") or ""
        m = re.search(r"/repos/([^/]+/[^/]+)$", rurl)
        if m:
            full = m.group(1)
        url = it.get("html_url") or f"https://github.com/{full}/issues/{it.get('number','')}"
        body_txt = re.sub(r"\s+", " ", (it.get("body") or "")).strip()
        title = it.get("title") or ""
        snip = truncate(body_txt) if body_txt else (it.get("state") or "")
        prefix = f"[{full}] " if full else ""
        results.append({"title": f"{prefix}{title}", "url": url, "snippet": snip})
    return results


def parse_api_code(body):
    """REST /search/code (needs token) -> unified results."""
    results = []
    for it in (json.loads(body).get("items") or []):
        path = it.get("path") or ""
        repo = it.get("repository", {}).get("full_name") or ""
        html_url = it.get("html_url") or f"https://github.com/{repo}/blob/HEAD/{path}"
        # 'matches' carry the raw fragment; else the path is the best snippet
        snip = ""
        for m in it.get("text_matches") or []:
            frag = m.get("fragment") or ""
            if frag:
                snip = truncate(re.sub(r"\s+", " ", frag))
                break
        if not snip:
            snip = path
        results.append({"title": f"{repo} · {path}", "url": html_url, "snippet": truncate(snip)})
    return results


def api_search(kind, query, count):
    """GitHub REST API search. Returns (results, message) — results empty on fail."""
    url = f"{API_BASE}/{kind}?q={urllib.parse.quote(query)}&per_page={count}"
    headers = ""  # remote curl will set its own UA
    status, body, msg = _ssh_fetch(url)

    if status != "ok":
        return [], f"github api {msg}"
    if msg.strip() != "200":
        if msg.strip() == "401" and kind == "code":
            return [], "code search requires authentication (401): no GITHUB_TOKEN and the web UI needs sign-in — use --type repositories/issues instead"
        if msg.strip() in ("403", "429"):
            return [], f"github api rate-limited (HTTP {msg}); try web fallback"
        return [], f"github api HTTP {msg}"

    try:
        data = json.loads(body)
    except (ValueError, AttributeError):
        return [], "github api returned unparseable JSON"

    if not data.get("items"):
        return [], ""  # empty, not an error
    if kind == "repositories":
        results = parse_api_repositories(body)
    elif kind == "issues":
        results = parse_api_issues(body)
    else:
        results = parse_api_code(body)
    return results, ""


# ---------------------------------------------------------------------------
# Web (server-rendered embeddedData JSON) parsing
# ---------------------------------------------------------------------------
def _extract_embedded_data(body):
    """Return the search payload dict from github.com/search embeddedData."""
    m = re.search(
        r'<script type="application/json" data-target="react-app.embeddedData">(.*?)</script>',
        body, re.S)
    if not m:
        return None
    try:
        # The results live under payload.blackbirdSearchRoute (loaded on the
        # search page); navigate to it so callers can read .results directly.
        payload = json.loads(m.group(1)).get("payload", {})
        return payload.get("blackbirdSearchRoute") or payload
    except (ValueError, AttributeError):
        return None


def parse_web_repositories(payload):
    results = []
    for r in payload.get("results") or []:
        hl = strip_tags(r.get("hl_name") or "")
        repo = r.get("repo", {}).get("repository", {})
        owner = repo.get("owner_login") or ""
        name = repo.get("name") or ""
        lang = r.get("language") or ""
        title = hl + (f" · {lang}" if lang else "")
        url = f"https://github.com/{owner}/{name}" if owner and name else ""
        if not url:
            continue
        snip = strip_tags(r.get("hl_trunc_description") or "") or f"{r.get('followers', '?')} followers"
        results.append({"title": title, "url": url, "snippet": truncate(snip)})
    return results


def parse_web_issues(payload):
    results = []
    for r in payload.get("results") or []:
        repo = r.get("repo", {}).get("repository", {})
        owner = repo.get("owner_login") or ""
        name = repo.get("name") or ""
        hl_title = strip_tags(r.get("hl_title") or "")
        num = r.get("number") or ""
        url = f"https://github.com/{owner}/{name}/issues/{num}" if owner and name and num else ""
        if not url:
            continue
        text = strip_tags(r.get("hl_text") or "")
        results.append({
            "title": f"[{owner}/{name}] {hl_title}",
            "url": url,
            "snippet": truncate(text) if text else (r.get("state") or ""),
        })
    return results


def parse_web_code(payload):
    """Code web results require login; normally return empty + sign-in gating."""
    results = []
    for r in payload.get("results") or []:
        path = r.get("path") or ""
        repo = r.get("repository", {})
        if isinstance(repo, dict):
            full = repo.get("id") or ""  # code web blob has different shape
        # fall back: emit path as snippet, repo not available in embedded payload shape
        if not path:
            continue
        results.append({
            "title": truncate(path),
            "url": f"https://github.com/search?q={urllib.parse.quote(path)}&type=code",
            "snippet": truncate(r.get("highlightsText") or ""),
        })
    return results


def web_search(kind, query, count):
    """github.com/search fallback. Returns (results, message)."""
    url = f"{WEB_BASE}?q={urllib.parse.quote(query)}&type={WEB_TYPE[kind]}"
    status, body, msg = _ssh_fetch(url)
    if status != "ok":
        return [], f"github web {msg}"
    if msg.strip() != "200":
        return [], f"github web HTTP {msg}"
    payload = _extract_embedded_data(body)
    if payload is None:
        return [], "github web search returned no embeddedData (blocked/JS-walled?)"
    if kind == "code" and payload.get("logged_in") is False:
        # code search on the web requires login for us (no token) -> honest gate
        return [], "github code web search requires sign-in (no GITHUB_TOKEN); use repository/issues search instead"
    if kind == "repositories":
        results = parse_web_repositories(payload)
    elif kind == "issues":
        results = parse_web_issues(payload)
    else:
        results = parse_web_code(payload)
    return results, "source: github web search"


# ---------------------------------------------------------------------------
# Orchestration
# ---------------------------------------------------------------------------
def main():
    signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    ap = argparse.ArgumentParser(
        description="GitHub search (repos/code/issues) via search-bot VPS jump host."
    )
    ap.add_argument("query", help="search query, e.g. \"modernc sqlite\"")
    ap.add_argument("--count", type=int, default=DEFAULT_COUNT,
                    help=f"max results (default {DEFAULT_COUNT}, capped at {MAX_COUNT})")
    ap.add_argument("--type", choices=["repositories", "code", "issues"],
                    default="repositories",
                    help="what to search (default repositories)")
    args = ap.parse_args()

    count = max(1, min(args.count, MAX_COUNT))
    start = time.monotonic()
    payload = {"status": "unavailable", "method": "github",
               "elapsed_ms": 0, "results": [], "count": 0}

    if not args.query.strip():
        payload["message"] = "empty query"
    else:
        # ---- 1. GitHub REST API. Results empty => err is "" (genuine 200-empty)
        #      or a failure message (non-200, code-no-token, rate-limit, parse). ----
        results, err = api_search(args.type, args.query, count)
        if results:
            payload["status"] = "ok"
            payload["results"] = results
            payload["count"] = len(results)
            payload["message"] = "source: github api"
        else:
            # ---- 2. Web fallback (repos/issues work; code requires sign-in). ----
            w_results, w_msg = web_search(args.type, args.query, count)
            if w_results:
                payload["status"] = "ok"
                payload["results"] = w_results
                payload["count"] = len(w_results)
                payload["message"] = w_msg
            elif err == "" and "requires" not in (w_msg or ""):
                # API genuinely succeeded with no results -> real empty
                payload["status"] = "empty"
                payload["message"] = "github api returned no results"
            else:
                # API failed AND/OR web gated -> honest unavailable. When both
                # messages carry the same auth/sign-in gate, keep just the API one.
                payload["status"] = "unavailable"
                auth_same = ("sign-in" in w_msg or "authentication" in w_msg
                             and "authentication" in err or "sign-in" in err)
                notes = [err] if auth_same else [n for n in (err, w_msg) if n]
                payload["message"] = truncate("; ".join(notes), 240) or "github search unavailable"

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
