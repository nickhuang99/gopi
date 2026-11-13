package agent

// selfnote_question_key_test.go — THE RETRIEVAL KEY IS NOT THE BODY (defect fix,
// 2026-11-02).
//
// WHAT WENT WRONG, measured on the HP ledger before this file existed:
//
//	select count(*), sum(question = answer) from distilled_qa where topic='self-record';
//	--  2284 | 2284
//	select count(*), sum(question = answer) from distilled_qa where topic='response';
//	--  8043 | 8041
//
// Both automatic capture paths passed the body as the QUESTION as well as the
// answer, so every captured row had question == answer. That is not a cosmetic
// duplication: `question` is the field memory search matches a query against, so
// the match surface became "your query versus two thousand characters of prose".
// Precision collapsed, and two rows with unrelated content ranked as near
// duplicates the moment they shared an opening line — "[TASK_LIST_BEGIN]" being
// the measured worst case (2105 rows on that ledger).
//
// WHAT THIS FILE PINS, in three arms:
//
//  1. THE KEY IS SHORT AND IS NOT THE BODY. For a note, for a long response and
//     for the task-list-receipt shape that produced the damage, the derived
//     question is bounded, non-empty, and different from the answer whenever the
//     body is longer than the bound.
//  2. THE KEY IS THE OPENING, DECORATION STRIPPED. A markdown heading, a list
//     bullet, a blockquote marker or a table separator must not become the key:
//     a key of "##" or "|---|---|" is worse than no key at all.
//  3. IT NEVER PRODUCES AN EMPTY KEY. memory_store refuses an empty question
//     outright, so a degenerate body would lose the whole row; the fallback is
//     the collapsed body capped to the same bound.
//
// The answer side is asserted UNCHANGED in arm 1: the fix must not cost the
// ledger a byte of captured content.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestMemoryQuestionKeyIsNotTheBody is the regression guard for the defect: before
// the fix, selfNoteStoreArgs returned the note twice and captureResponseText
// passed `body` twice, so question == answer for every row.
func TestMemoryQuestionKeyIsNotTheBody(t *testing.T) {
	// The exact shape that filled the ledger with unusable keys: a task-list
	// receipt is long, opens with a bracketed marker, and is NOT an experience.
	receipt := "[TASK_LIST_BEGIN]\n[\n" +
		`{"task_id":"1","summary":"read the briefing","status":"progressing"}` + "\n" +
		`{"task_id":"2","summary":"run the smallest reproduction","status":"pending"}` + "\n" +
		"]\n[TASK_LIST_END]\n" +
		"这一轮把清单交上去了，其余没有别的。"

	note := "根因不是去重算法，而是写入时 question 字段被赋成了整段正文：" +
		"memoryStoreRow 的 question 与 answer 来自同一个字符串，于是检索键被毁。"
	long := strings.Repeat("这一段是被捕获的回答正文，它应该原样留在 answer 里。", 60)

	cases := []struct {
		name string
		body string
		call func(string) string
	}{
		{"note-args", note, func(s string) string {
			_, q, _, _ := selfNoteStoreArgs(s)
			return q
		}},
		{"note-answer-unchanged", note, func(s string) string {
			_, _, a, _ := selfNoteStoreArgs(s)
			return a
		}},
		{"receipt", receipt, memoryQuestionKey},
		{"long-response", long, memoryQuestionKey},
	}

	for _, tc := range cases {
		got := tc.call(tc.body)
		if got == "" {
			t.Fatalf("%s: derived an EMPTY key — the store would refuse the row", tc.name)
		}
		if n := utf8.RuneCountInString(got); n > memoryQuestionKeyRunes {
			t.Fatalf("%s: key is %d runes, over the %d bound: %q",
				tc.name, n, memoryQuestionKeyRunes, got)
		}
	}

	// The answer side keeps every byte: the fix must not cost content.
	_, _, answer, _ := selfNoteStoreArgs(note)
	if answer != note {
		t.Fatalf("selfNoteStoreArgs changed the ANSWER — the fix must only derive a key:\n got %q\nwant %q",
			answer, note)
	}

	// And the key genuinely differs from the body once the body exceeds the bound.
	for _, body := range []string{receipt, long} {
		if utf8.RuneCountInString(body) <= memoryQuestionKeyRunes {
			continue
		}
		if memoryQuestionKey(body) == body {
			t.Fatalf("the key is byte-identical to a %d-rune body — the defect is back",
				utf8.RuneCountInString(body))
		}
	}
}

// TestMemoryQuestionKeyStripsOpeningDecoration pins that a line made only of
// markdown/box decoration never becomes the key: a key of "##" or "|---|" would
// match everything and mean nothing.
func TestMemoryQuestionKeyStripsOpeningDecoration(t *testing.T) {
	const want = "根因是写入时把整段正文当成了检索键"
	bodies := []string{
		"## " + want,
		"- " + want,
		"> " + want,
		"**" + want + "**",
		"┌──────────────┐\n│ " + want + "\n└──────────────┘",
		"| 结论 | 说明 |\n|---|---|\n| " + want + " | 实测 |",
		"### \n\n" + want,
	}
	for _, body := range bodies {
		got := memoryQuestionKey(body)
		if strings.HasPrefix(got, "#") || strings.HasPrefix(got, "|") ||
			strings.HasPrefix(got, ">") || strings.HasPrefix(got, "-") ||
			strings.HasPrefix(got, "┌") || strings.HasPrefix(got, "│") {
			t.Fatalf("key kept its opening decoration: %q (body %q)", got, body)
		}
		if !strings.Contains(got, want) {
			t.Fatalf("key lost the content: %q (body %q)", got, body)
		}
	}
}

// TestMemoryQuestionKeyNeverEmptyForARealBody is the refusal-arm guard, stated
// precisely: a body that CARRIES CONTENT must always yield a key, because
// memory_store refuses an empty question and the whole captured row would be
// lost. What this arm does NOT claim is that an empty body yields a key — it
// must not. Inventing a key for a bodyless capture would smuggle a row with no
// content past the store's own contract, which is the opposite of the fix.
func TestMemoryQuestionKeyNeverEmptyForARealBody(t *testing.T) {
	for _, body := range []string{
		"##\n---\n***\n真的内容在这一行",
		"│││\n│ 边框里的一句话 │",
		"| 列A | 列B |\n|---|---|\n| 真正的结论 | 实测 |",
		strings.Repeat("。", 400) + "收尾的一句话",
		"\n\n\n  空白之后才有东西",
	} {
		if got := memoryQuestionKey(body); got == "" {
			t.Fatalf("body %q yielded an empty key — the row would be refused", body)
		}
	}
}

// TestMemoryQuestionKeyEmptyBodyStaysEmpty pins the OTHER side of the same
// contract: a body with nothing in it produces no key, so the store refuses the
// row for the reason it already states. A fabricated key here would let an
// empty capture reach the ledger.
func TestMemoryQuestionKeyEmptyBodyStaysEmpty(t *testing.T) {
	for _, body := range []string{"", "   ", "\n\n\n"} {
		if got := memoryQuestionKey(body); got != "" {
			t.Fatalf("empty body %q produced the key %q — an empty capture must be refused by the store, not smuggled past it",
				body, got)
		}
	}
}
