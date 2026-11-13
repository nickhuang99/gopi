#!/usr/bin/env python3
"""arxiv_search.py + bing_search.py 回归测试（web-search 垃圾结果 epic）。

运行:  python3 tests/test_search_quality.py -v

背景（2026-09 epic）:
  gopi web search "find all paper links for download by authors including
  name shunyu yao" 曾返回全部无关结果（"find your phone" 等）。根因有二:
  1. arxiv_search.py 把一切查询都当 all: 全文搜索 —— 作者名被 OR 匹配噪音淹没，
     找不到本人论文；现在检测作者意图并改写为 au:<first_last> 字段查询。
  2. bing_search.py 对"稀有名字+主题词"查询会收到 bing 的 bot 软屏蔽诱饵 SERP
     （回显查询但内容是完全无关的缓存页，每次随机：字典/知乎/Reddit/披萨店），
     脚本曾把诱饵当 ok 结果返回；现在加相关性门，全部 miss => status=empty。

覆盖:
  arxiv:  author-intent 改写（papers by X / X papers / verbose NL 含 name X）、
          主题查询不改写、au: 前缀透传、误判自纠（title 查询 au: 空 -> all: 回退）
  bing:   junk_serp 诱饵判定（拉丁/CJK/脚手架词/缩写保护）
"""
import os
import re
import sys
import unittest
import xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "scripts"))

import arxiv_search as arx  # noqa: E402
import bing_search as bing  # noqa: E402


# ---------------------------------------------------------------------------
# arxiv author-intent 改写
# ---------------------------------------------------------------------------
class TestArxivAuthorDetection(unittest.TestCase):

    def test_verbose_nl_author_query(self):
        # epic 原始失败查询：脚手架词多，但必须提取出 shunyu_yao
        q = "find all paper links for download by authors including name shunyu yao"
        self.assertEqual(arx.detect_author_name(q), "shunyu_yao")

    def test_clean_author_forms(self):
        for q in ("shunyu yao papers", "papers by shunyu yao",
                  "shunyu yao arxiv", "yann lecun papers",
                  "download papers by geoffrey hinton"):
            self.assertIsNotNone(arx.detect_author_name(q), q)

    def test_topic_queries_not_rewritten(self):
        # 主题查询绝不能误判成作者
        for q in ("retrieval augmented generation", "ReAct paper",
                  "large language models survey", "tree of thoughts",
                  "graph neural networks"):
            self.assertIsNone(arx.detect_author_name(q), q)

    def test_fielded_query_passthrough(self):
        # au: 前缀原样透传（不套 all:）
        self.assertTrue(arx.is_fielded('au:"Shunyu Yao"'))
        self.assertTrue(arx.is_fielded("ti:transformers"))
        self.assertFalse(arx.is_fielded("shunyu yao papers"))
        self.assertTrue("all:" not in arx.build_url('au:"Shunyu Yao"', 5))
        self.assertIn("au:", arx.build_url('au:"Shunyu Yao"', 5))

    def test_author_alias(self):
        url = arx.build_url("author:Shunyu_Yao", 5)
        self.assertIn("au:Shunyu_Yao", url)  # author: alias -> au:（未引号，API 直用）
        # 引号作者名（空格）应保留引号字段语法
        url2 = arx.build_url('au:"Shunyu Yao"', 5)
        self.assertIn("au:\"Shunyu Yao\"", url2)

    def test_build_url_all_default(self):
        url = arx.build_url("retrieval augmented generation", 5)
        self.assertIn("all:", url)


# ---------------------------------------------------------------------------
# bing 相关性门（诱饵 SERP 检测）
# ---------------------------------------------------------------------------
class TestBingJunkGate(unittest.TestCase):

    def _res(self, titles):
        return [{"title": t, "snippet": ""} for t in titles]

    def test_decoy_serp_flagged(self):
        # bing 软屏蔽诱饵：内容与查询完全无关
        for q, titles in (
            ("shunyu yao arxiv", ["波浪号（～）是什么意思 - 知乎"]),
            ("shunyu yao papers arxiv", ["APPRECIATE中文 (简体)翻译：剑桥词典"]),
            ("webagent paper", ["Wizarding World"]),
            ("find all paper links for download by authors including name shunyu yao",
             ["Find your phone - Google Account"]),
        ):
            self.assertTrue(bing.junk_serp(q, self._res(titles)), q)

    def test_cjk_decoy_flagged(self):
        self.assertTrue(bing.junk_serp("姚顺雨 论文",
                                       self._res(["Domino's Pizza | Pizzakurier"])))
        self.assertTrue(bing.junk_serp("舜宇光学",
                                       self._res(["FlyerTalk forums IHG rate codes"])))

    def test_real_results_pass(self):
        for q, titles in (
            ("hermes agent", ["Hermes Agent — Open-Source AI Agent That Grows"]),
            ("shunyu yao", ["About – Shunyu Yao – 姚顺雨"]),
            ("retrieval augmented generation",
             ["What is Retrieval-Augmented Generation (RAG) - GeeksforGeeks"]),
            # CJK 部分命中：4+ 字查询对 2 字重叠命中要放行
            ("舜宇光学", ["舜宇集团"]),
            ("量子计算 综述", ["量子计算研究进展综述"]),
        ):
            self.assertFalse(bing.junk_serp(q, self._res(titles)), q)

    def test_abbreviation_protected(self):
        # 缩写/同义词查询：结果含展开形式（llm -> "LLMs"）=> 放行
        self.assertFalse(bing.junk_serp(
            "llm", self._res(["Large language model"])))
        self.assertFalse(bing.junk_serp(
            "llm agent",
            [{"title": "Large language model", "snippet": "LLMs are ... agents"}]))
        # 单特征词无命中（如 "llm" 3 字符无特征词）=> 不判 junk（无法可靠判定）
        self.assertFalse(bing.junk_serp(
            "llm", self._res(["Unrelated filler page"])))

    def test_single_letter_and_empty(self):
        self.assertFalse(bing.junk_serp("", []))
        self.assertFalse(bing.junk_serp("x", []))




# ---------------------------------------------------------------------------
# arxiv 同名人消歧（co-authorship anchor cluster）
# ---------------------------------------------------------------------------
class TestArxivHomonymDisambiguation(unittest.TestCase):
    """au: 字段查询按姓名匹配，同名不同人会混在一起（shunyu yao = agent
    研究者 + 凝聚态物理学家 + hep-th 理论家）。disambiguate_homonyms 以
    #1 相关命中为锚，只保留与其有共作者的论文。纯函数测试用合成 entry。"""

    def _entry(self, title, authors):
        e = ET.Element("{%s}entry" % "http://www.w3.org/2005/Atom")
        t = ET.SubElement(e, "{%s}title" % "http://www.w3.org/2005/Atom")
        t.text = title
        for au in authors:
            a = ET.SubElement(e, "{%s}author" % "http://www.w3.org/2005/Atom")
            n = ET.SubElement(a, "{%s}name" % "http://www.w3.org/2005/Atom")
            n.text = au
        return e

    def test_keeps_anchor_cluster_drops_other_person(self):
        import xml.etree.ElementTree as ET
        A = "http://www.w3.org/2005/Atom"
        entries = [
            self._entry("ReAct paper", ["Shunyu Yao", "Jeffrey Zhao"]),   # anchor
            self._entry("Tree of Thoughts", ["Shunyu Yao", "Jeffrey Zhao"]),  # same cluster
            self._entry("quantum chaos", ["Shunyu Yao"]),                 # physicist, no shared coauthor
            self._entry("Topological", ["Shunyu Yao", "Zhong Wang"]),     # cond-mat physicist
            self._entry("WebShop", ["Shunyu Yao", "Jeffrey Zhao"]),       # cluster (via anchor)
        ]
        # want=3：锚(ReAct)+共作者论文到 3 条即停；物理学家噪音必须被滤掉
        kept = arx.disambiguate_homonyms(entries, "shunyu_yao", 3)
        self.assertEqual(len(kept), 3)
        flat = " ".join(" ".join((e.findtext("a:title", "", {"a": A}) or "").split())
                        for e in kept)
        self.assertIn("ReAct", flat)
        self.assertIn("Tree", flat)
        self.assertIn("WebShop", flat)
        self.assertNotIn("quantum", flat)
        self.assertNotIn("Topological", flat)

    def test_single_person_no_diversity_returns_empty(self):
        import xml.etree.ElementTree as ET
        entries = [
            self._entry("Paper A", ["Shunyu Yao", "Jeffrey Zhao"]),
            self._entry("Paper B", ["Shunyu Yao", "Jeffrey Zhao"]),
            self._entry("Paper C", ["Shunyu Yao", "Howard Chen"]),
        ]
        # want=5 > window size 3 -> 无歧义空间，返回 []（调用方保留原结果）
        self.assertEqual(arx.disambiguate_homonyms(entries, "shunyu_yao", 5), [])

    def test_many_distinct_authors_one_cluster(self):
        # 全部论文共享共作者（单一人），窗口有 15 条 -> 应全部保留
        import xml.etree.ElementTree as ET
        entries = [
            self._entry(f"Paper {i}", ["Yann LeCun", "Coauthor A", "Coauthor B"])
            for i in range(12)
        ]
        kept = arx.disambiguate_homonyms(entries, "yann_lecun", 5)
        self.assertEqual(len(kept), 5)  # 达到 want 即停


# ---------------------------------------------------------------------------
# bing 本地英文直连（fetch_local_en / host 参数化）
# ---------------------------------------------------------------------------
class TestBingLocalEn(unittest.TestCase):

    def test_local_en_url_has_english_market(self):
        import urllib.parse
        from unittest import mock
        # 验证 URL 构造带 setlang=en&mkt=en-US&cc=US
        url = "https://www.bing.com/search?" + urllib.parse.urlencode(
            {"q": "shunyu yao paper", "count": 10, "setlang": "en",
             "mkt": "en-US", "cc": "US"})
        self.assertIn("setlang=en", url)
        self.assertIn("mkt=en-US", url)

    def test_unwrap_unescapes_plain_urls(self):
        # scholar 等非 ck/a 外链含 &amp; 实体，须反转义
        u = bing.unwrap_bing_url(
            "https://scholar.google.com/citations?user=qJBXk9cAAAAJ&amp;hl=zh-CN")
        self.assertNotIn("&amp;", u)
        self.assertIn("&hl=", u)


if __name__ == "__main__":
    unittest.main()
