"""Regression guard for the alleged '1-3 char term' junk-gate bug (t-...). 
Verifies: surname-only hit ('yao' in 'S Yao' co-author strings) passes the 
gate — i.e. distinctive-term threshold does NOT reject real results whose 
only overlap with the query is a short token."""
import importlib.util, os, sys
sys.path.insert(0, os.path.join(os.path.dirname(__file__)))
import unittest

HERE = os.environ.get("BING_SCRIPT", os.path.expanduser(
    "/home/user/.gopi/plugins/websearch/scripts/bing_search.py"))
spec = importlib.util.spec_from_file_location("bing_search", HERE)
bing = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bing)


class TestShortTermGate(unittest.TestCase):
    def test_terms_split_drops_stopword_paper(self):
        self.assertEqual(bing.query_terms("shunyu yao paper"), ["shunyu", "yao"])

    def test_surname_only_hit_passes_gate(self):
        # real SERP where the long token never appears; only 'yao' in co-author
        res = [{"title": "S Yao, Efficient adaptation of LLMs",
                "url": "https://arxiv.org/abs/2103.10385", "snippet": "S Yao and J Doe"}]
        self.assertFalse(bing.junk_serp("shunyu yao paper", res))

    def test_all_miss_with_distinctive_terms_is_junk(self):
        res = [{"title": "APPRECIATE - Cambridge Dictionary",
                "url": "https://dict.cambridge.org/x", "snippet": "to respect"}]
        self.assertTrue(bing.junk_serp("shunyu yao paper", res))

    def test_no_distinctive_terms_gate_disabled(self):
        self.assertFalse(bing.junk_serp("yao q", [
            {"title": "unrelated", "url": "https://x.com", "snippet": ""}]))


if __name__ == "__main__":
    unittest.main()
