#!/usr/bin/env python3
"""Scenario-level regression test for bing_search.py — REAL captured SERP HTML.

Closes the hole the existing unit tests leave: test_search_quality.py feeds the
junk gate HAND-BUILT result dicts, so it can never catch a defect in the part
that turns a real page into those dicts (parse_results + the source chain +
finalize). That is exactly where the 2026-10 breakage lived:

  1. `gopi web search "golang" --method bing --count 3` reported status=empty
     "all bing sources failed or returned decoy" while the same SERP was a
     REAL result page ("The Go Programming Language" first). Judging a SERP —
     and requesting it — from the caller's narrow --count made a genuine page
     look like the decoy; the query token need not appear in the first few
     results (go.dev pages) even though it appears later and in the URL
     (golang.google.cn).
  2. Conversely, the 716 audit's defect: a decoy page (query echoed in the
     search box, results unrelated and random per request — bing's bot
     soft-block) must NEVER be reported as ok results.

Both are scenario defects of the whole script, so the guard works on CAPTURED
pages through the real parser + real gate + real finalize.

Fixtures (plugins/websearch/tests/testdata/, captured 2026-10-08 from the live
endpoint, trimmed to the `#b_results > li.b_algo` blocks so no CSS/JS bulk is
committed):
  bing_real_golang.html   — genuine SERP for "golang" (10 b_algo blocks)
  bing_decoy_serp.html    — decoy SERP for "shunyu yao arxiv" (echoed query,
                            unrelated results: map/navigation tutorials)

Run: python3 tests/test_bing_scenario.py -v
Env: BING_SCRIPT=<path> selects the script under test (used by the red-proof
     harness to point at a pre-fix revision; default ../scripts/bing_search.py).
"""
import importlib.util
import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_SCRIPT = os.path.normpath(os.path.join(HERE, "..", "scripts", "bing_search.py"))
SCRIPT = os.environ.get("BING_SCRIPT") or DEFAULT_SCRIPT


def load_script(path):
    spec = importlib.util.spec_from_file_location("bing_search_under_test", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


bing = load_script(SCRIPT)

TESTDATA = os.path.join(HERE, "testdata")
REAL_PAGE = os.path.join(TESTDATA, "bing_real_golang.html")
DECOY_PAGE = os.path.join(TESTDATA, "bing_decoy_serp.html")

REAL_QUERY = "golang"
DECOY_QUERY = "shunyu yao arxiv"
REAL_FIRST_URL = "https://golang.google.cn/"


def load(path):
    with open(path, "r", errors="replace") as fh:
        return fh.read()


def verdict(page_path, query, count):
    """Exactly what main() does for one source: parse then finalize."""
    res = bing.parse_results(load(page_path), count)
    return bing.finalize({"status": "unavailable", "method": "bing",
                          "results": [], "count": 0}, res, query, count)


class TestCapturedRealSerp(unittest.TestCase):
    """A genuine SERP must come back ok — including at the narrow --count the
    caller asked for (the 2026-10 regression)."""

    def test_captured_real_page_parses(self):
        res = bing.parse_results(load(REAL_PAGE), 10)
        self.assertEqual(len(res), 10, "10 b_algo blocks in the captured page")
        self.assertEqual(res[0]["title"], "The Go Programming Language")
        self.assertEqual(res[0]["url"], REAL_FIRST_URL)
        self.assertTrue(all(r["url"].startswith("http") for r in res))

    def test_real_serp_ok_at_count3_dot_regression(self):
        """--count 3 on a genuine page ⇒ status=ok with 3 real results."""
        p = verdict(REAL_PAGE, REAL_QUERY, 3)
        self.assertEqual(p["status"], "ok", "real SERP reported as %s: %s" % (
            p["status"], p.get("message")))
        self.assertEqual(p["count"], 3)
        self.assertEqual(p["results"][0]["url"], REAL_FIRST_URL)

    def test_gate_judges_exactly_what_is_reported(self):
        """The gate's sample == the reported sample: whatever the caller sees,
        the gate saw. (Guards the fix in BOTH directions — a narrower judged
        window lets decoys through, a wider one rejects real pages.)"""
        for count in (3, 5, 10):
            res = bing.parse_results(load(REAL_PAGE), count)
            self.assertEqual(len(res), count)
            gate_said_junk = bing.junk_serp(REAL_QUERY, res)
            self.assertEqual(verdict(REAL_PAGE, REAL_QUERY, count)["status"],
                             "empty" if gate_said_junk else "ok",
                             "count=%d: verdict must follow the gate on the "
                             "same sample" % count)


class TestCapturedDecoySerp(unittest.TestCase):
    """A decoy/challenge page must NEVER be reported as ok results."""

    def test_captured_decoy_page_is_flagged(self):
        res = bing.parse_results(load(DECOY_PAGE), 10)
        self.assertGreaterEqual(len(res), 5, "the decoy page is structurally a SERP")
        self.assertTrue(bing.junk_serp(DECOY_QUERY, res),
                        "unrelated results must trip the gate")

    def test_verdict_is_empty_not_ok(self):
        p = verdict(DECOY_PAGE, DECOY_QUERY, 5)
        self.assertEqual(p["status"], "empty")
        self.assertEqual(p["count"], 0)
        self.assertEqual(p["results"], [])
        self.assertIn("soft-block", p["message"])

    def test_decoy_page_is_empty_at_every_count(self):
        for count in (3, 5, 10):
            p = verdict(DECOY_PAGE, DECOY_QUERY, count)
            self.assertEqual(p["status"], "empty",
                             "count=%d leaked decoy results: %r" % (count, p["results"]))

    def test_source_chain_skips_the_decoy_source(self):
        """main()'s per-source predicate: empty parse OR junk ⇒ try the next
        source. A decoy page must never satisfy it as ok."""
        decoy_res = bing.parse_results(load(DECOY_PAGE), 5)
        real_res = bing.parse_results(load(REAL_PAGE), 5)
        self.assertTrue((not decoy_res) or bing.junk_serp(DECOY_QUERY, decoy_res),
                        "decoy source must be skipped")
        self.assertFalse((not real_res) or bing.junk_serp(REAL_QUERY, real_res),
                         "real source must be accepted")


if __name__ == "__main__":
    print("script under test: %s" % SCRIPT)
    unittest.main()
