package tools

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Literal JSON-escape repair at the write funnel.
//
// The defect these tests pin: a producer that serialized a payload and stored
// the SERIALIZATION (python json.dumps without ensure_ascii=False; any
// double-encode) leaves the literal escape text in the column —
// `\u6210\u672c\u5206\u6790` instead of 成本分析 — and the row's jieba_token
// (the only CJK-gram index column) is empty, so the row is unrecallable.
// Measured on ~/.gopi/memory.db 2026-09-23: ids 4663–4668 carry exactly this
// shape in topic+question+answer+search_kw with jieba_token length 0.
//
// RED before the change (the escaped payload is stored verbatim), GREEN after.
// The conservative half — a field whose real CJK merely DISCUSSES an escape
// (ids 8/1018/4680/4681/4682/4700/4714 in the same DB) — must stay
// byte-identical: those rows are prose, not payload.
// ---------------------------------------------------------------------------

// escapedTopic is the payload shape the repair targets: the JSON-escaped form
// of "成本分析".
const escapedTopic = `\u6210\u672c\u5206\u6790`

// escapedQuestion mixes ASCII dates with escaped CJK, exactly like the real
// rows (payload coverage 0.81–0.93 of the field's non-space runes).
const escapedQuestion = `2026-09-21 \u5468\u4e00 DeepSeek \u8d26\u5355\u7684\u771f\u5b9e\u7528\u91cf\u662f\u591a\u5c11\uff1f`

// escapedAnswer is escaped CJK prose long enough to pass the garbage gate.
const escapedAnswer = `\u603b\u8f93\u5165 11.78 \u4ebf token\uff0c\u7f13\u5b58\u547d\u4e2d\u7387\u4ece 98% \u4e0b\u964d\u5230 41%\uff0c` +
	`\u9519\u5cf0\u7b56\u7565\u662f\u628a\u6279\u91cf\u4efb\u52a1\u632a\u5230\u591c\u95f4\u4fbf\u5b9c\u65f6\u6bb5\u3002`

// escapedKeyword is the natural phrasing a user searches with, in escaped form
// ("挪到夜间").
const escapedKeyword = `\u632a\u5230\u591c\u95f4`

// TestLiteralEscapePayloadDecodedOnStore is the store-funnel regression test:
// an escaped payload must reach the DB as readable CJK, with a non-empty
// jieba_token carrying the real 2-char grams.
func TestLiteralEscapePayloadDecodedOnStore(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore(escapedTopic, escapedQuestion, escapedAnswer, "bill-analysis",
		[]string{escapedKeyword, "deepseek billing"}); err != nil {
		t.Fatal(err)
	}

	db := openRawDB(t, path)
	defer db.Close()
	var topic, question, answer, searchKw, jieba string
	if err := db.QueryRow(
		"SELECT topic, question, answer, search_kw, jieba_token FROM distilled_qa WHERE id=1").
		Scan(&topic, &question, &answer, &searchKw, &jieba); err != nil {
		t.Fatal(err)
	}

	if topic != "成本分析" {
		t.Errorf("topic not decoded: got %q want %q", topic, "成本分析")
	}
	for _, want := range []string{"周一", "账单", "真实用量", "多少"} {
		if !strings.Contains(question, want) {
			t.Errorf("question missing decoded %q: %q", want, question)
		}
	}
	for _, want := range []string{"缓存命中率", "错峰策略", "夜间"} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer missing decoded %q: %q", want, answer)
		}
	}
	if !strings.Contains(searchKw, "挪到夜间") {
		t.Errorf("search_kw not decoded: %q", searchKw)
	}
	if jieba == "" {
		t.Fatal("jieba_token is EMPTY — the CJK-gram index column was never filled")
	}
	// The grams of the decoded CJK must exist as index tokens (the query side
	// speaks exactly these 2-char fragments).
	for _, gram := range []string{"成本", "分析", "账单", "夜间"} {
		if !strings.Contains(jieba, gram) {
			t.Errorf("jieba_token missing decoded gram %q: %q", gram, jieba)
		}
	}
	if strings.Contains(topic, memoryEscapeRunPrefix) || strings.Contains(jieba, memoryEscapeRunPrefix) {
		t.Errorf("escape text survived the store funnel: topic=%q jieba=%q", topic, jieba)
	}
}

// TestLiteralEscapeProseMentionUntouched is the conservative half: a field whose
// REAL CJK discusses an escape sequence (the shape of ids 8 and 1018) is prose,
// never a payload — it must be stored byte-identical.
func TestLiteralEscapeProseMentionUntouched(t *testing.T) {
	path := setMemoryDB(t)

	// question: NO real CJK, one escape mention inside a long English sentence
	// (payload coverage ≈0.05 — the coverage guard alone must reject it).
	const proseQ = `How to pretty-print tool input JSON showing literal \u003e and \u0026 instead of the real characters, while preserving key order?`
	// answer: REAL CJK that names the escapes (the row-8/1018 shape).
	const proseA = "gopi-log-reader 的 formatToolInput 走 json.MarshalIndent；Go 的 encoding/json 会把尖括号与和号转成 " +
		`\u003c \u003e \u0026 这样的转义序列，所以调试视图里看到的是转义文本而不是字符本身。`

	if _, err := MemoryStore("escape-discussion", proseQ, proseA, "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	var question, answer string
	if err := db.QueryRow(
		"SELECT question, answer FROM distilled_qa WHERE id=1").Scan(&question, &answer); err != nil {
		t.Fatal(err)
	}
	if question != proseQ {
		t.Errorf("prose question was rewritten:\n got %q\nwant %q", question, proseQ)
	}
	if answer != proseA {
		t.Errorf("prose answer was rewritten:\n got %q\nwant %q", answer, proseA)
	}
}

// TestLiteralEscapePayloadDecodedOnUpdate pins the same repair on the update
// funnel, and that the re-derived jieba_token follows the DECODED text.
func TestLiteralEscapePayloadDecodedOnUpdate(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("placeholder topic", "what does the update funnel do with escaped payloads",
		"an answer body long enough to pass the garbage gate unchanged", "test", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryUpdate(1, MemoryUpdateFields{
		Topic:    escapedTopic,
		Question: escapedQuestion,
		Answer:   escapedAnswer,
		Keywords: []string{escapedKeyword},
	}); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	var topic, searchKw, jieba string
	if err := db.QueryRow(
		"SELECT topic, search_kw, jieba_token FROM distilled_qa WHERE id=1").Scan(&topic, &searchKw, &jieba); err != nil {
		t.Fatal(err)
	}
	if topic != "成本分析" {
		t.Errorf("update did not decode topic: %q", topic)
	}
	if !strings.Contains(searchKw, "挪到夜间") {
		t.Errorf("update did not decode search_kw: %q", searchKw)
	}
	if !strings.Contains(jieba, "账单") || strings.Contains(jieba, memoryEscapeRunPrefix) {
		t.Errorf("update left a stale/escaped jieba_token: %q", jieba)
	}
}

// TestDecodeLiteralEscapesBoundaries pins the predicate itself (pure function):
// what decodes, what stays, and why — the coverage/prose/floor rules.
func TestDecodeLiteralEscapesBoundaries(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "no escapes here", "no escapes here"},
		{"full payload", `\u6210\u672c\u5206\u6790`, "成本分析"},
		{"mixed payload", `DeepSeek \u8d26\u5355\u7684\u771f\u5b9e\u7528\u91cf\u662f\u591a\u5c11`,
			"DeepSeek 账单的真实用量是多少"},
		{"prose with real CJK", `Go 的 encoding/json 输出 \u003c \u003e \u0026 转义`,
			`Go 的 encoding/json 输出 \u003c \u003e \u0026 转义`},
		{"prose without CJK, low coverage",
			`literal \u003e and \u0026 inside a long English sentence that merely mentions escapes`,
			`literal \u003e and \u0026 inside a long English sentence that merely mentions escapes`},
		{"single run below the floor", `\u6210`, `\u6210`},
		{"two runs is already a payload", `\u6210\u672c`, "成本"},
		{"control escape substituted", `\u0000\u0001 payload chars`, "\uFFFD\uFFFD payload chars"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decodeLiteralEscapes(c.in); got != c.want {
				t.Errorf("decodeLiteralEscapes(%q):\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}
