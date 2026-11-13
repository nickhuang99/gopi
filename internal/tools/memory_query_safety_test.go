package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Query safety: NO user query may produce an engine (SQL/FTS5) error.
// ---------------------------------------------------------------------------
//
// THE DEFECT (measured 2026-09-28 on gopi 10dcd490, branch
// task/01a0e477a731700d9274d269337ce239). The mechanics are FTS5's: a raw
// MATCH term containing '-' is not a literal but a COLUMN QUALIFIER
// (`term:col` / `term-col`), so `token-free` asks for a column named `free`
// and the engine refuses the whole statement —
//
//	sqlite> select count(*) from f where f match 'token-free';
//	Error: stepping, no such column: free (1)
//
// WHAT WAS ACTUALLY BROKEN IS NARROWER THAN THAT MECHANICS SUGGESTS, and the
// difference was MEASURED through the real CLI rather than assumed (an earlier
// draft of this file asserted the broad form and the RED run refuted it):
//
//	pre-fix `gopi memory search token-free`  → 13 hits, no error
//	pre-fix `gopi memory search 网络-测试`    → SQL logic error: no such column: 测试 (1)
//	pre-fix `gopi memory search VPN-网络测试环境` → SQL logic error: no such column: 网络测试环境 (1)
//
// escapeQuery already quoted every PURE-ASCII field; the defect was its
// PER-FIELD CJK exemption — `if hasCJK(f) { append(f) }` — which left any
// field CONTAINING CJK completely UNQUOTED, hyphen and all (commit 3aff9c4b,
// "escapeQuery CJK balance"). The quote-stripping that ran before that test
// also let `"` alone reach FTS5 as an unterminated string, and a no-term query
// built the empty MATCH expression (a syntax error).
//
// The boundary set below therefore covers BOTH halves: the ASCII shapes that
// must keep working (they did before, and a fix that regressed them would be
// worse than the bug) and the CJK-bearing / quote / no-term shapes that were
// the live failure.
//
// THE CONTRACT THIS FILE PINS: for ANY input, the surface answers with either
// a structured hit list / the empty-hint (0 hits) or an explicit, intended
// validation error — NEVER an engine error. "No distilled memory found" is a
// legitimate answer; "SQL logic error" never is.
//
// The RED proof for this file is recorded in the task report
// (devdoc/evidence/…-red-proof.txt and the `## Evidence (self-filed)` section):
// with the pre-fix sources restored, the CJK-bearing members fail with the
// `SQL logic error: no such column: …` text quoted above and the pure-ASCII
// members pass — the boundary set is what made the narrow root cause visible.
//
// NOTE ON PURPOSE: this file deliberately calls ONLY the public entry points
// (MemorySearch / MemorySearchHits) — no internal helper — so the pre-fix
// source still COMPILES against it. That is what makes the RED observation an
// assertion failure (the real symptom) instead of a compile error.

// querySafetySamples is the boundary sample set: the hyphen family from the
// report, the quote/operator grammar, CJK, and the no-term / oversized shapes.
// Each entry's wantErr is the ONLY permitted error kind — an explicit
// validation refusal, never an engine error.
var querySafetySamples = []struct {
	name  string
	query string
	// noTerm marks a query that carries NO searchable term at all (empty,
	// whitespace, quotes/punctuation only). Such a query must answer as 0 hits
	// — the empty hint — and the literal empty string with the explicit
	// validation refusal; never an engine error.
	noTerm   bool
	wantHits bool // a seeded row must be recalled (the fix must not break recall)
}{
	// --- the '-' family: ASCII members (quoted pre-fix, must STAY working)
	// and the CJK-bearing members that were the live failure (left raw by the
	// per-field hasCJK exemption) ---
	{name: "token-free", query: "token-free", wantHits: true},
	{name: "chat-context", query: "chat-context", wantHits: true},
	{name: "read-only", query: "read-only", wantHits: true},
	{name: "no-op", query: "no-op", wantHits: true},
	{name: "ascii hyphenated with tail separator", query: "no-"},
	{name: "leading hyphen", query: "-free"},
	{name: "double hyphen", query: "x--y"},
	{name: "mixed CJK+ASCII hyphenated", query: "VPN-网络测试环境"},
	{name: "CJK hyphen CJK", query: "网络-测试", wantHits: true},

	// --- quotes and the FTS5 operator grammar ---
	{name: "quoted hyphenated phrase", query: `"token-free"`, wantHits: true},
	{name: "mid-word quote", query: `memory"search`},
	{name: "apostrophe", query: `a'b`},
	{name: "star is a literal inside a phrase", query: "memory*"},
	{name: "paren", query: "(x)"},
	{name: "OR operator", query: "a OR b"},
	{name: "NOT operator", query: "NOT x"},
	{name: "NEAR operator", query: "NEAR(a b)"},
	{name: "column filter", query: "topic:x"},
	{name: "caret range", query: "a^b"},
	{name: "unterminated quote only", query: `"`},
	{name: "stray backslash", query: `a\b`},

	// --- CJK ---
	{name: "pure CJK", query: "网络测试环境", wantHits: true},
	{name: "CJK mixed with ASCII", query: "网络测试环境 mtu", wantHits: true},

	// --- no-term shapes ---
	{name: "empty", query: "", noTerm: true},
	{name: "whitespace only", query: "   ", noTerm: true},
	{name: "quotes only", query: `""`, noTerm: true},

	// --- oversized: the declared-safe degradation (terms are capped) ---
	{name: "oversized ASCII", query: strings.Repeat("word ", 2000)},
	{name: "oversized CJK", query: strings.Repeat("网络测试环境", 200)},
}

// engineErrorMarkers are the substrings that mean "the engine refused the
// statement" — the exact failure mode this fix removes. The check is on the
// TEXT (not on err != nil), because the reported bug reached the CLI as a
// rendered error string.
var engineErrorMarkers = []string{
	"SQL logic",
	"fts5:",
	"syntax error",
	"no such column",
	"unterminated string",
}

// TestMemorySearchQuerySafety is the RED→GREEN regression for the query-safety
// contract above: every boundary sample answers with structure or 0 hits, and
// no sample can produce an engine error.
func TestMemorySearchQuerySafety(t *testing.T) {
	setMemoryDB(t)
	// Seed the rows the hyphen/CJK samples must recall — one per topic so a
	// hit can be attributed to its own record.
	seeds := []struct{ topic, question, answer string }{
		{"token-free", "how do I run a token-free query?", "quote the term so '-' cannot reach the FTS5 grammar."},
		{"chat-context", "what is chat-context?", "the console session's own transcript."},
		{"read-only", "why read-only?", "search opens the ledger with query_only."},
		{"no-op", "when is the bump a no-op?", "an old schema without the counters."},
		{"网络测试环境", "网络测试环境如何验证?", "用 trigram 与 2-char LIKE 两条召回路由验证。"},
		{"网络-测试", "网络-测试连字符怎么办?", "连字符必须被引号中和，否则 FTS5 当作列限定符。"},
	}
	for _, s := range seeds {
		if _, err := MemoryStore(s.topic, s.question, s.answer, "", nil); err != nil {
			t.Fatalf("seed %q: %v", s.topic, err)
		}
	}

	for _, tc := range querySafetySamples {
		t.Run(tc.name, func(t *testing.T) {
			out, err := MemorySearch(tc.query, 5, "")
			// A query that trims to nothing is the ONE documented refusal
			// (there is no empty MATCH expression that is not a syntax error).
			if strings.TrimSpace(tc.query) == "" {
				if err == nil {
					t.Fatalf("query %q must be refused, got %d hits", tc.query, countHits(out))
				}
				if !strings.Contains(err.Error(), "must not be empty") {
					t.Fatalf("query %q: the refusal must be the explicit empty-query error, got: %v", tc.query, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("query %q: unexpected error: %v", tc.query, err)
			}
			for _, marker := range engineErrorMarkers {
				if strings.Contains(out, marker) {
					t.Fatalf("query %q leaked the engine error marker %q into its output:\n%s", tc.query, marker, out)
				}
			}
			if tc.noTerm {
				if n := countHits(out); n != 0 {
					t.Fatalf("query %q carries no searchable term: expected a clean 0-hit answer, got %d hits:\n%s", tc.query, n, out)
				}
				return
			}
			if tc.wantHits && countHits(out) == 0 {
				t.Errorf("query %q recalled nothing; the seeded row must still be reachable:\n%s", tc.query, out)
			}
		})
	}
}

// TestMemorySearchHyphenPhraseRecall is the minimal-sample separation of the
// fix (Rule 10 habit 4): the SINGLE reported string must both stop erroring
// AND still recall a row that spells it — quoting must not turn a match into
// a miss. 'token-free' is one query; the row carries it verbatim.
func TestMemorySearchHyphenPhraseRecall(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("token-free",
		"how do I search token-free?",
		"the '-' must be quoted, or FTS5 reads the tail as a column name.",
		"", nil); err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("token-free", 5, "")
	if err != nil {
		t.Fatalf("`memory search token-free` must not fail: %v", err)
	}
	if !strings.Contains(out, "token-free") {
		t.Fatalf("hyphenated query did not recall its own row:\n%s", out)
	}
}

// TestMemorySearchHostileInputNoEngineError drives the same contract through
// the STRUCTURED exit (MemorySearchHits) and the procedural merge, so a fix
// that only guarded the rendered wrapper cannot pass: the JSON/short-circuit
// path is what the agent loop consumes.
func TestMemorySearchHostileInputNoEngineError(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("vpn lesson", "vpn split tunnel drops?", "raise the mtu", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"token-free", `a'b`, "no-", "memory*", "(x)", "NOT x", "a OR b", "VPN-网络测试环境",
		`"`, `a\b`, "topic:x", "网络-测试",
	} {
		if _, err := MemorySearchHits(q, 5, ""); err != nil {
			t.Errorf("MemorySearchHits(%q) returned an error: %v", q, err)
		}
		if _, err := MemorySearch(q, 5, ""); err != nil {
			t.Errorf("MemorySearch(%q) returned an error: %v", q, err)
		}
		if _, err := ProceduralSearch(q, 5); err != nil {
			t.Errorf("ProceduralSearch(%q) returned an error: %v", q, err)
		}
	}
}

// TestMemorySearchOverlappingCJKRunsFollowedByASCII is the grammar hole a
// single-field spell would leave open: the pre-fix code left a field carrying
// ANY CJK bare, so `deepseek-开支` (ASCII+CJK+hyphen) reached FTS5 as the raw
// text and died with `no such column: 开支`. Splitting the field into its CJK
// run and its ASCII run closes it, and the record must still be recalled by
// the CJK half.
func TestMemorySearchOverlappingCJKRunsFollowedByASCII(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("深seek 开支", "怎么降低deepseek开支?", "把批量任务挪到夜间，避开高峰电价。", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("deepseek-开支", 5, "")
	if err != nil {
		t.Fatalf("mixed ASCII+CJK hyphenated query must not fail: %v", err)
	}
	if !strings.Contains(out, "deepseek") {
		t.Fatalf("mixed query did not recall its row:\n%s", out)
	}
}

// TestMemorySearchCJKFragmentsStillRecall guards the OTHER half of the fix:
// quoting/spelling the ASCII side must not cost CJK recall. A stored
// contiguous CJK run must stay reachable by fragments of itself (the
// jieba_token 2-gram index + the trigram route), whether the fragment lives in
// the question or only in the answer.
//
// MEASURED (2026-09-28) — this test replaces a guess: the briefing named "a
// contiguous CJK run is one unicode61 token" as a KNOWN GAP to file. On a
// store whose jieba_token column is populated it does NOT manifest: every
// fragment of 网络测试环境 (the segmenter's own words 网络/测试环境, and the
// whole run) was recalled even when the text appears ONLY in the answer,
// because jieba_token carries the segmented words and distilled_qa_tri
// carries ≥3-char substrings. The residue that IS filed: a fragment the
// SEGMENTER does not produce (a lone character, or a substring crossing a
// word boundary like 络测 — see devdoc/jieba-token-real-segmentation.md for
// the measured boundary), and a LEGACY row whose jieba_token is empty
// (imported by direct SQL, past the store helper).
func TestMemorySearchCJKFragmentsStillRecall(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("vpn mtu",
		"vpn split tunnel drops under load?",
		"答案只在这里出现：网络测试环境测得。", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"网络测试环境", "测试环境", "网络测", "环境", "网络"} {
		out, err := MemorySearch(fragment, 5, "")
		if err != nil {
			t.Fatalf("CJK fragment %q errored: %v", fragment, err)
		}
		if countHits(out) == 0 {
			t.Errorf("CJK fragment %q (present only in the answer) recalled nothing:\n%s", fragment, out)
		}
	}
}

// TestMemorySearchOversizedQueryIsCapped: a pathological query degrades to a
// bounded statement instead of an unbounded one — it must answer (0 hits is a
// valid answer) and must not emit an engine error. The bound itself is a
// constant (memsearchMaxMatchTerms / memsearchMaxCJKRouteTerms); this test
// pins the BEHAVIOUR, not the number.
func TestMemorySearchOversizedQueryIsCapped(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("oversize probe", "does a huge query break search?", "no: terms are capped.", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		strings.Repeat("zz-token ", 4000), // ASCII, hyphenated
		strings.Repeat("网络测试环境", 300),     // CJK, no separators
		strings.Repeat("词-", 3000),        // CJK+separator, the widest term fan-out
		strings.Repeat("a", 200) + "-" + strings.Repeat("b", 200),
	} {
		if !utf8.ValidString(q) {
			t.Fatal("fixture must be valid UTF-8")
		}
		out, err := MemorySearch(q, 5, "")
		if err != nil {
			t.Fatalf("oversized query (len %d) must degrade, not fail: %v", len(q), err)
		}
		for _, marker := range engineErrorMarkers {
			if strings.Contains(out, marker) {
				t.Fatalf("oversized query (len %d) leaked %q:\n%.200s", len(q), marker, out)
			}
		}
	}
	// Direct exit too (the agent loop's path).
	if _, err := MemorySearchHits(strings.Repeat("zz-token ", 4000), 5, ""); err != nil {
		t.Fatalf("MemorySearchHits must also degrade: %v", err)
	}
}
