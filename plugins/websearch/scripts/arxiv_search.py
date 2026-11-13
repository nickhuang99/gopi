#!/usr/bin/env python3
"""arXiv paper search — 0-token websearch plugin script (W3).

Queries the arXiv export API directly (reachable from China, no VPN) and
returns a single slim JSON object. All filtering/truncation happens inside
this script so the agent only pays tokens for the distilled result.

Usage:
  python3 arxiv_search.py "retrieval augmented generation" [--count 5]

Output (single JSON object on stdout, always):
  {"status": "ok|empty|timeout|unavailable", "method": "arxiv",
   "elapsed_ms": N,
   "results": [{"title": "...", "url": "...",
                "abstract_snippet": "...", "published": "YYYY-MM-DD"}],
   "count": N}

status semantics (design doc gopi-websearch-plugin-design.md):
  ok          — >=1 result found
  empty       — query succeeded, no results
  timeout     — arxiv API did not answer within API_TIMEOUT_SEC
  unavailable — any other failure (HTTP error, unparseable XML, ...)
"""
import argparse
import json
import re
import socket
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

# ---------------------------------------------------------------------------
# Constants — single source of truth, no magic numbers inline (Rule 1)
# ---------------------------------------------------------------------------
DEFAULT_COUNT = 5        # results returned when --count is omitted
MAX_COUNT = 20           # hard cap so the JSON payload stays slim (0-token)
SNIPPET_CHARS = 300      # abstract truncation length
API_TIMEOUT_SEC = 15     # network timeout; exceeded -> status=timeout
API_BASE_URL = "https://export.arxiv.org/api/query"
USER_AGENT = "GoPiWebSearch/1.0 (arxiv_search)"

ATOM_NS = "http://www.w3.org/2005/Atom"
OPENSEARCH_NS = "http://a9.com/-/spec/opensearch/1.1/"
XML_NS = {"a": ATOM_NS, "os": OPENSEARCH_NS}


# arXiv field prefixes — a query already starting with one is passed through
# VERBATIM (fielded search), so callers can use au:"Full Name", ti:..., etc.
# The bare "author:" alias maps to au: (friendlier for LLM callers).
ARXIV_FIELD_RE = re.compile(r"^(?:au|ti|abs|all|cat|co|jr|rn|cn|author):", re.I)

# Author-intent detection: a plain query that names an author with paper
# context ("papers by shunyu yao", "shunyu yao papers arxiv", ...) is
# rewritten to au:<first_last> so arxiv's author field is searched instead
# of full text (all: on a rare author name OR-matches noise and misses the
# person's actual papers — the exact bug this epic hit).
PAPER_CTX_RE = re.compile(
    r"\b(?:papers?|publications?|arxiv|works?|research|preprints?)\b", re.I)

# Homonym disambiguation (au: fielded author search is *name-only*, so a
# plain au:first_last query can mix several DISTINCT people who share the
# name — e.g. "Shunyu Yao" returns the Princeton agent-researcher AND a
# condensed-matter physicist AND a hep-th theorist (93 papers, ~5 people,
# all valid au: matches). arXiv has no author-id; we disambiguate by
# co-authorship cluster (a real person's papers share collaborators):
#   - fetch a wider au: window (HOMONYM_WINDOW) sorted by relevance;
#   - anchor = the #1 relevance hit (the query's paper context strongly
#     biases relevance to the intended person — "shunyu yao paper" ranks
#     ReAct first);
#   - keep only papers sharing >=1 co-author with the anchor's growing set
#     (transitive within the window); drop the orphaned other-person papers.
# Network cost: 1 extra API call on the au: rewrite path (the wide fetch),
# ~0.5s — acceptable for author queries. Single-person names (yann_lecun)
# keep all top hits anyway (anchor cluster == top-N), so no result is lost.
HOMONYM_WINDOW = 15       # au: disambiguation fetch window (>= count*3)

# Author stopwords that cannot be part of an author name — a token walk past a
# marker word (by/of/from/name/...) stops at the first of these, so a
# verbose query like "find all paper links for download by authors
# including name shunyu yao" yields 'shunyu_yao' rather than grabbing
# scaffolding words.
# Common topic words that must never be read as a person's name tokens in
# patterns 2/3 of detect_author_name ("retrieval augmented generation",
# "neural network", "attention is all you need" are topics, not people).
COMMON_TOPIC_WORDS = frozenset(
    "augmented generation retrieval neural network networks deep learning "
    "machine model models attention is all you need it a an the of for with "
    "and or how does do what why when where which best top new recent state "
    "survey tutorial benchmark datasets dataset code github python llm llms "
    "gpt bert transformer transformers vision language image text embedding "
    "embeddings agent agents reinforcement policy training inference prompt "
    "reasoning chain thought tree search tool tools use using".split())

AUTHOR_STOPWORDS = frozenset(
    "find all paper papers links link for download by authors author including "
    "name of from arxiv list show me the and with on their his her my whose who "
    "are is research works work publications download papers article articles "
    "about related".split())


def is_fielded(query):
    """True when the query already carries an arXiv field prefix (au:/ti:/...)."""
    return bool(ARXIV_FIELD_RE.match(query.strip()))


def parse_authors(entry):
    """Extract the author name list of one Atom entry."""
    return [a.findtext("a:name", "", XML_NS) for a in entry.findall("a:author", XML_NS)]


def disambiguate_homonyms(entries, author_name, want):
    """Keep only the co-authorship cluster of the anchor (top-relevance) hit.

    entries: list of ET entries from an au:<name> fetch (already relevance-
    sorted by the API). Returns the kept entries (<= want), or [] when the
    window shows no diversity (single person — caller keeps its results).
    author_name is the au: value (e.g. 'shunyu_yao') for display only.
    """
    if len(entries) <= want:
        return []                       # 窗口内无歧义空间
    top = entries[0]
    display = author_name.replace("_", " ").title()
    anchor_authors = set(parse_authors(top)) - {display}
    # 完全无共作者（异常数据）=> 不判歧义
    if not anchor_authors:
        return []
    kept = [top]
    for entry in entries[1:]:
        au = set(parse_authors(entry))
        if au & anchor_authors:
            kept.append(entry)
            anchor_authors |= au - {display}
        if len(kept) >= want:
            break
    return kept


def _wrap(entry):
    """Wrap one Atom entry in a minimal feed root (parse_entries input)."""
    root = ET.Element("{%s}feed" % ATOM_NS.rstrip("}"))
    root.append(entry)
    return root


def _looks_technical(w):
    """Acronym/CamelCase token (ReAct, GPT, LoRA) — a technique, not a name."""
    return len(w) > 1 and (w.isupper() or (w[0].isupper() and any(c.isupper() for c in w[1:])))


def detect_author_name(query):
    """Detect an author name in a plain (non-fielded) query.

    Returns the name with spaces->underscore, lowercased (arXiv au: format,
    e.g. 'shunyu_yao'), or None when the query does not clearly name an
    author. Pattern: a marker word (name|author|named|by|of|from|including)
    followed by 2+ consecutive non-stopword tokens — the author name; or a
    query starting with "Name Name papers|arxiv|publications|works".
    Conservative on purpose: topic queries ("retrieval augmented
    generation", "ReAct paper") yield None and stay on the all: path.
    """
    toks = re.findall(r"[A-Za-z][A-Za-z.\-':]*", query)
    markers = {"name", "author", "named", "by", "of", "from", "including",
               "does", "did", "is", "was", "are", "were"}
    for i, t in enumerate(toks):
        if t.lower() in markers and i + 1 < len(toks):
            name = []
            for w in toks[i + 1:]:
                if w.lower() in AUTHOR_STOPWORDS:
                    break
                name.append(w)
            # cap the name at 2-3 person tokens: "does shunyu yao handle
            # ambiguity" -> name=shunyu_yao, not shunyu_yao_handle_ambiguity
            # "does shunyu yao handle ambiguity" -> shunyu_yao: cut trailing
            # lowercase tokens that look like verbs/objects (2+ remaining).
            while len(name) > 2 and name[-1].islower() and len(name[-1]) > 2 \
                    and name[-1] not in ("jr", "ii", "iii", "van", "von", "de", "der"):
                name = name[:-1]
            if len(name) >= 2 and not any(
                    w.lower() in COMMON_TOPIC_WORDS or _looks_technical(w)
                    for w in name):
                return "_".join(name).lower()
    # 2) "Name Name" (2-3 tokens, case-insensitive) immediately before a
    #    paper-context word: "shunyu yao paper", "shunyu yao arxiv".
    #    Name tokens must not be common topic words (else "retrieval
    #    augmented generation" style topics would match).
    ctx = PAPER_CTX_RE.search(query)
    if ctx:
        words = query[:ctx.start()].split()
        words = [w for w in words if re.fullmatch(r"[A-Za-z][A-Za-z.\-']*", w)]
        if 2 <= len(words) <= 3 and not any(
                w.lower() in COMMON_TOPIC_WORDS or _looks_technical(w)
                for w in words):
            return "_".join(words).lower()
    # 2b) "Name Name and colleagues" / "Name Name et al" suffix form
    m = re.search(
        r"\b([A-Za-z][A-Za-z.\-']*(?:\s+[A-Za-z][A-Za-z.\-']+)+)"
        r"\s+(?:and\s+colleagues|et\s+al\.?)\b", query)
    if m:
        words = m.group(1).split()
        if (2 <= len(words) <= 3
                and not any(w.lower() in COMMON_TOPIC_WORDS for w in words)):
            return "_".join(words).lower()
    # 3) bare author query: the whole query is just 2-3 non-topic tokens
    #    ("shunyu yao") with no context word at all.
    words = query.split()
    if (2 <= len(words) <= 3
            and all(re.fullmatch(r"[A-Za-z][A-Za-z.\-']*", w) for w in words)
            and not any(w.lower() in COMMON_TOPIC_WORDS or _looks_technical(w)
                        for w in words)):
        return "_".join(words).lower()
    return None


def build_url(query, count):
    """Build the arxiv API query URL (relevance sort, descending).

    Fielded queries (au:/ti:/abs:/... or the author: alias) pass through
    verbatim; anything else gets the all: full-text prefix (relevance sort).
    """
    q = query.strip()
    if q.lower().startswith("author:"):
        q = "au:" + q[len("author:"):]
    if is_fielded(q):
        search_query = q
    else:
        search_query = "all:" + urllib.parse.quote(q)
    params = {
        "search_query": search_query,
        "max_results": str(count),
        "sortBy": "relevance",
        "sortOrder": "descending",
    }
    return API_BASE_URL + "?" + "&".join(
        f"{k}={v}" for k, v in params.items()
    )


def fetch(url):
    """GET the arxiv API; raises on failure (timeout vs other handled by caller)."""
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(req, timeout=API_TIMEOUT_SEC) as resp:
        return resp.read()


def clean_text(text):
    """Strip and collapse whitespace (arxiv XML wraps titles/summaries)."""
    if text is None:
        return ""
    return " ".join(text.strip().split())


def snippet(text):
    """Truncate abstract to SNIPPET_CHARS, appending '...' when cut."""
    text = clean_text(text)
    if len(text) > SNIPPET_CHARS:
        return text[:SNIPPET_CHARS] + "..."
    return text


def parse_entries(xml_bytes):
    """Parse the Atom feed into the slim result list."""
    root = ET.fromstring(xml_bytes)
    results = []
    for entry in root.findall("a:entry", XML_NS):
        title = clean_text(entry.findtext("a:title", "", XML_NS))
        raw_id = clean_text(entry.findtext("a:id", "", XML_NS))
        if "/abs/" in raw_id:
            arxiv_id = re.sub(r"v\d+$", "", raw_id.split("/abs/")[-1])
        else:
            arxiv_id = raw_id
        results.append({
            "title": title,
            "url": "https://arxiv.org/abs/" + arxiv_id,
            "abstract_snippet": snippet(entry.findtext("a:summary", "", XML_NS)),
            "published": clean_text(entry.findtext("a:published", "", XML_NS))[:10],
        })
    return results


def main():
    ap = argparse.ArgumentParser(
        description="Search arXiv papers, return slim JSON (0-token plugin)."
    )
    ap.add_argument("query", help="search query, e.g. \"retrieval augmented generation\"")
    ap.add_argument("--count", type=int, default=DEFAULT_COUNT,
                    help=f"max results (default {DEFAULT_COUNT}, capped at {MAX_COUNT})")
    args = ap.parse_args()

    count = max(1, min(args.count, MAX_COUNT))
    start = time.monotonic()
    payload = {"status": "unavailable", "method": "arxiv",
               "elapsed_ms": 0, "results": [], "count": 0}

    query = args.query.strip()
    rewritten = ""
    if query.lower().startswith("author:"):
        # bare author: alias -> au: fielded search
        rewritten = query
        query = "au:" + query[len("author:"):]
    elif query and not is_fielded(query):
        # Author-intent detection on plain queries: only when a paper-context
        # word is present AND a 2+ word name sits right next to it ("papers
        # by shunyu yao", "shunyu yao arxiv papers"). Conservative — ordinary
        # topic queries stay on the all: full-text path.
        if PAPER_CTX_RE.search(query):
            name = detect_author_name(query)
            if name:
                rewritten = query
                query = f"au:{name}"
    if rewritten:
        payload["query_rewritten"] = rewritten

    try:
        xml_bytes = fetch(build_url(query, count))
        results = parse_entries(xml_bytes)
        # Self-correction: a wrongly-detected "author" (e.g. a paper title
        # like "tree of thoughts paper" -> au:tree_of_thoughts) returns zero
        # author hits — fall back to the original plain query's all: search.
        if not results and rewritten and query.startswith("au:"):
            query = rewritten  # original plain query
            payload.pop("query_rewritten", None)
            xml_bytes = fetch(build_url(query, count))
            results = parse_entries(xml_bytes)
        elif results and query.startswith("au:"):
            # Homonym disambiguation: a bare au: name can match several
            # distinct people. Fetch a wider window, keep only the anchor's
            # co-authorship cluster (see disambiguate_homonyms), and serve
            # that instead of the mixed-name top-N.
            try:
                wide_xml = fetch(build_url(query, HOMONYM_WINDOW))
                wide = ET.fromstring(wide_xml).findall("a:entry", XML_NS)
                name = query[len("au:"):].strip().strip('"')
                kept = disambiguate_homonyms(wide, name, count)
                if kept:
                    # disambiguate_homonyms returns [] only when no diversity
                    # (then we keep the plain top-N); non-empty = the anchor
                    # cluster — parse those entries directly.
                    wide_root = ET.fromstring(wide_xml)
                    results = [r for e in kept
                               for r in parse_entries(ET.tostring(_wrap(e)))]
            except Exception:  # noqa: BLE001 — 歧义消解失败则回落原结果
                pass
        payload["status"] = "ok" if results else "empty"
        payload["results"] = results
        payload["count"] = len(results)
    except (socket.timeout, TimeoutError) as e:
        payload["status"] = "timeout"
        payload["message"] = f"arxiv API timeout after {API_TIMEOUT_SEC}s: {e}"
    except urllib.error.HTTPError as e:
        payload["message"] = f"arxiv API HTTP {e.code}: {e.reason}"
    except urllib.error.URLError as e:
        if isinstance(e.reason, (socket.timeout, TimeoutError)):
            payload["status"] = "timeout"
            payload["message"] = f"arxiv API timeout: {e.reason}"
        else:
            payload["message"] = f"arxiv API unreachable: {e.reason}"
    except ET.ParseError as e:
        payload["message"] = f"arxiv API returned unparseable XML: {e}"
    except Exception as e:  # noqa: BLE001 — plugin must always emit JSON
        payload["message"] = f"arxiv search failed: {e!r}"

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
