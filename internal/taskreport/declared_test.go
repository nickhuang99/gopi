package taskreport

// declared_test.go — the report-verdict reader, pinned against the REAL report
// that produced it.
//
// The fixture bodies are the actual opening lines of row 01a186b9-064c's report
// and of a counter-proof report (the shape row 01a18244 protects). They are
// copied verbatim rather than paraphrased: the predicate's whole job is to
// recognise an author's own sentence, so a fixture that re-words it would test
// the re-wording instead.

import (
	"strings"
	"testing"
)

// declaredBlockedReport is the head of 01a186b9's own report (2026-10-29), the
// run this reader was written for: the clone channel measured an empty ground
// and the row closed `completed` while this file said the work was never done.
const declaredBlockedReport = "# Task Report: 删光 `gopi task reopen` —— 本轮裁决 BLOCKED（前置仍未落地），交付物 = 实测取证 + 可执行删除计划\n" +
	"\n" +
	"**Verdict: BLOCKED** — 两条由 briefing 写死的硬前置在 master 上仍**双双未落地**，\n" +
	"briefing 明令「若任一未完成，本任务停下来，在报告里写明 blocked on <id>，不要自行动手」。\n" +
	"本轮**没有**删除任何代码、**没有**提交、**没有**移动 ref；工作树干净。\n" +
	"\n" +
	"## Completed\n" +
	"\n" +
	"1. 查明「没有内容可交付」的成因。\n"

// declaredCounterProofReport is the shape that must NOT be judged unfinished: a
// run asked to produce no change, which produced none and says so — the
// items=0 case row 01a18244 fixed into a legitimate completion.
const declaredCounterProofReport = "# Task Report: count the zero rows\n" +
	"\n" +
	"**Verdict: GREEN AS FOUND** — the counter-proof holds; nothing needed changing.\n" +
	"\n" +
	"## Completed\n" +
	"\n" +
	"1. Ran the count; it is 0 on today's tree.\n"

// TestDeclaredIncompleteReadsTheAuthorsVerdict is the primary arm: the measured
// report declares the run unfinished, and the sentence is handed back verbatim
// (with the author's own reason after the dash, because that reason is what a
// reader of the row needs).
func TestDeclaredIncompleteReadsTheAuthorsVerdict(t *testing.T) {
	sentence, ok := DeclaredIncomplete(declaredBlockedReport)
	if !ok {
		t.Fatalf("the measured report opens `**Verdict: BLOCKED**` and must be read as a declaration; "+
			"a reader that misses it is the defect this file pins:\n%s", declaredBlockedReport)
	}
	if !strings.HasPrefix(sentence, "BLOCKED") {
		t.Fatalf("the declared sentence must open with the author's own verdict word, got %q", sentence)
	}
	// The sentence is QUOTED into a ledger row, so Markdown emphasis must not
	// survive into it — the measured e2e read `BLOCKED** — upstream is not on
	// master` before this arm existed.
	if strings.ContainsAny(sentence, "*_`") {
		t.Fatalf("the quoted sentence must carry no Markdown emphasis, got %q", sentence)
	}
	if !strings.Contains(sentence, "前置") {
		t.Fatalf("the author's own tail must survive verbatim, got %q", sentence)
	}
}

// TestDeclaredIncompleteCounterProofIsNotUnfinished is the arm that keeps the
// repair from re-opening row 01a18244: a run that completed BY MEASURING made no
// admission, so it keeps its `completed` close.
func TestDeclaredIncompleteCounterProofIsNotUnfinished(t *testing.T) {
	if sentence, ok := DeclaredIncomplete(declaredCounterProofReport); ok {
		t.Fatalf("a counter-proof report declares no unfinished work — got %q", sentence)
	}
}

// TestDeclaredIncompleteIsLineAnchored refuses the false-positive direction: the
// word inside prose is not a verdict, because a mention is not a declaration.
func TestDeclaredIncompleteIsLineAnchored(t *testing.T) {
	body := "# Report\n\nThe earlier run was BLOCKED, and Verdict: BLOCKED appears in our notes.\n"
	if sentence, ok := DeclaredIncomplete(body); ok {
		t.Fatalf("a mid-line mention is not a verdict line — got %q", sentence)
	}
}

// TestDeclaredIncompleteVocabulary walks the declared spellings so a new one
// cannot be added to the list without a case that proves it fires, and so the
// WHOLE-WORD boundary is pinned (`BLOCKEDX` is not `BLOCKED`).
func TestDeclaredIncompleteVocabulary(t *testing.T) {
	for _, body := range []string{
		"Verdict: BLOCKED\n",
		"**Verdict: BLOCKED** — upstream prerequisites missing\n",
		"- Verdict: NOT DONE — the surface was not exercised\n",
		"Verdict: INCOMPLETE: 2 of 5 items\n",
		"Verdict: ABORTED (the run died on connect)\n",
		"> Verdict: BLOCKED-BY-UPSTREAM\n",
	} {
		if _, ok := DeclaredIncomplete(body); !ok {
			t.Errorf("must read as a declaration: %q", body)
		}
	}
	for _, body := range []string{
		"Verdict: DONE\n",
		"Verdict: GREEN AS FOUND\n",
		"Verdict: BLOCKEDX\n",
		"# Report with no verdict line\n",
		"",
	} {
		if sentence, ok := DeclaredIncomplete(body); ok {
			t.Errorf("must NOT read as a declaration (%q) — got %q", body, sentence)
		}
	}
}
