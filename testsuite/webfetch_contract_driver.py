#!/usr/bin/env python3
"""webfetch_contract_driver — mock-subprocess harness for the web_fetch.py
contract tests (testsuite/webfetch_contract_test.go).

ZERO NETWORK: every subprocess (ssh/curl) is monkeypatched at the module
level, so the contract suite exercises web_fetch.py's REAL parse/route/clean
logic against canned byte payloads — no ssh to searchbot, no curl, no DNS.

Usage:
  python3 webfetch_contract_driver.py route <url> [<url> ...]
      Print "<url>|<route>" per probe (real _pick_route logic).

  python3 webfetch_contract_driver.py fetch <case>
      Run web_fetch.py's main() with sys.argv and subprocess.run patched for
      <case>; print the resulting payload JSON (single line) to stdout.

Cases (fetch):
  ssh-single-line  marker + ONE-line minified body (the example.com class of
                   bug the old lines[2] indexing silently dropped)
  ssh-multiline    marker + multi-line body
  ssh-http500      HTTP 500 -> status=error
  ssh-000          curl http_code 000 -> status=unavailable
  ssh-timeout      curl rc 28 -> status=timeout
  ssh-nomarker     payload without the @@HTTP marker -> status=unavailable
  ssh-binary       NUL bytes in body -> status=error (non-text)
  ssh-truncated    long body + small --max-chars -> truncated tail marker
  direct-ok        DIRECT_DOMAINS host fetched via local curl -> ok,
                   message "content-type: text/html; direct"
  direct-fail      direct curl fails -> status=unavailable + "(direct route)"
                   (NO silent ssh fallback — geo-correctness contract)
"""
import importlib.util
import io
import json
import os
import subprocess
import sys
import types

_HERE = os.path.dirname(os.path.abspath(__file__))
_SCRIPT = os.environ.get(
    "WEBFETCH_SCRIPT",
    os.path.join(_HERE, "..", "plugins", "websearch", "scripts", "web_fetch.py"))


def load_module():
    spec = importlib.util.spec_from_file_location("webfetch_contract_mod", _SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)  # __name__ != "__main__" -> main() not run
    return mod


def _proc(rc, stdout):
    return types.SimpleNamespace(returncode=rc, stdout=stdout, stderr=b"")


# case -> (patched subprocess.run behavior, url, extra argv)
# URL choice drives the route: example.com = ssh, docs.bigmodel.cn = direct.
SSH_BODY_SINGLE = (b'@@HTTP:200|text/html RC:0\n'
                   b'<html><head><script>var a=1;</script><style>.x{}</style>'
                   b'</head><nav>menu-chrome</nav><body>'
                   b'<p>single-line-body-regression-7f3a</p>'
                   b'<div>A&amp;B</div></body></html>')
CASES = {
    "ssh-single-line": (lambda cmd, **kw: _proc(0, SSH_BODY_SINGLE),
                        "https://example.com/page", []),
    "ssh-multiline": (lambda cmd, **kw: _proc(
        0, b'@@HTTP:200|text/html RC:0\n<html>\n<body>\n<h1>multi</h1>\n'
           b'<p>line-body-ok-994d</p>\n</body>\n</html>\n'),
        "https://example.com/page", []),
    "ssh-http500": (lambda cmd, **kw: _proc(
        0, b'@@HTTP:500|text/html RC:0\n<html><body>boom</body></html>'),
        "https://example.com/page", []),
    "ssh-000": (lambda cmd, **kw: _proc(0, b'@@HTTP:000| RC:7\n'),
                "https://example.com/page", []),
    "ssh-timeout": (lambda cmd, **kw: _proc(0, b'@@HTTP:200|text/html RC:28\nlate'),
                    "https://example.com/page", []),
    "ssh-nomarker": (lambda cmd, **kw: _proc(0, b'garbage without marker'),
                     "https://example.com/page", []),
    "ssh-binary": (lambda cmd, **kw: _proc(
        0, b'@@HTTP:200|application/pdf RC:0\n%PDF-1.4\x00\x00binary'),
        "https://example.com/doc.pdf", []),
    "ssh-truncated": (lambda cmd, **kw: _proc(
        0, b'@@HTTP:200|text/html RC:0\n<p>' + b"x" * 5000 + b'</p>'),
        "https://example.com/long", ["--max-chars", "300"]),
    "direct-ok": (lambda cmd, **kw: _proc(
        0, b'<html><body><p>direct-cny-price-31c9</p></body></html>'),
        "https://docs.bigmodel.cn/pricing", []),
    "direct-fail": (lambda cmd, **kw: _proc(6, b""),  # curl: couldn't resolve
                    "https://docs.bigmodel.cn/pricing", []),
}


def run_fetch(case):
    mod = load_module()
    fake_run, url, extra = CASES[case]

    # ZERO-NETWORK: every subprocess.run inside web_fetch.py resolves to the
    # canned response — nothing can reach ssh/curl/DNS (a signature-mismatch
    # call would raise TypeError and fail the case loudly).
    mod.subprocess.run = lambda cmd, **kw: fake_run(cmd, **kw)

    argv = ["web_fetch.py", url] + extra
    old_argv, old_out = sys.argv, sys.stdout
    sys.argv, sys.stdout = argv, io.StringIO()
    try:
        mod.main()
        out = sys.stdout.getvalue()
    finally:
        sys.argv, sys.stdout = old_argv, old_out
    payload = json.loads(out.strip().splitlines()[-1])
    print(json.dumps(payload, ensure_ascii=False))


def main():
    if len(sys.argv) < 2:
        print("usage: driver route <url>... | fetch <case>", file=sys.stderr)
        return 2
    if sys.argv[1] == "route":
        mod = load_module()
        for u in sys.argv[2:]:
            try:
                print("%s|%s" % (u, mod._pick_route(u)))
            except Exception as e:
                print("%s|ERROR:%s" % (u, e))
        return 0
    if sys.argv[1] == "fetch":
        if len(sys.argv) != 3 or sys.argv[2] not in CASES:
            print("unknown fetch case: %r" % sys.argv[2:], file=sys.stderr)
            return 2
        run_fetch(sys.argv[2])
        return 0
    print("unknown mode: %r" % sys.argv[1], file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
