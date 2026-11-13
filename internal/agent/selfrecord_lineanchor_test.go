package agent

// selfrecord_lineanchor_test.go — A NOTE BEGINS AT THE START OF A LINE, and a
// marker the model MENTIONS mid-sentence is not a note.
//
// THE DEFECT THIS PINS (measured, not inferred). extractSelfNotes found the open
// marker with a bare strings.Index and paired it with the next close marker
// anywhere in the text — no position rule at all. So a round whose answer body
// TALKS ABOUT the contract (this repository's answers do exactly that, constantly)
// was cut at the mention inside a sentence, and the fragment between the mention
// and the round's real closer was written to self-record.md as that round's note.
//
// The measurement, on session 9adf61bc-c3a9-40be-9367-13dba0ef71b2 (this host,
// 2026-11-11):
//
//	self-record.md lines 1 and 2 begin "t1 ` 已经不是原文…" and "t2 ` 正文。…" —
//	both are MID-SENTENCE fragments of the answer body (the first 434 and 1,126
//	runes of the round's real note were cut away), and line 3 is a third one.
//
// THE RULE. The taught shape puts each delimiter on its own line, so a note's
// open AND close markers are LINE-ANCHORED — nothing but horizontal whitespace
// may precede them on their line. A marker found anywhere else is the model
// naming the contract, and it is neither a block's start nor one's end.
//
// fixtures used here and why they are bytes off disk rather than hand-typed:
//
//   - selfrecord-narrative-round.txt is round #160 of that session, verbatim:
//     an answer body that quotes the open marker inside a sentence and closes
//     with a REAL anchored block. It is the exact shape that produced the
//     fragments, so the red half of this file is a measurement, not a scenario.
//   - selfrecord-fragment-round.txt is round #225 of the same session: the body
//     quotes the open marker TWICE and its real block is anchored, which is the
//     pairing rule's own edge (an inline mention must not consume the closer).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// narrativeRoundFixture is the measured round that produced the record-file
// fragments; the assertion below is about ITS bytes, so the file is the fixture.
const narrativeRoundFixture = "selfrecord-narrative-round.txt"

// fragmentRoundFixture is the second measured round: two inline mentions and one
// anchored block.
const fragmentRoundFixture = "selfrecord-fragment-round.txt"

// selfRecordFixtureText reads one committed fixture and fails the test when it is
// missing — a fixture that cannot be read must not turn an assertion green.
func selfRecordFixtureText(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the recorded round (%s): %v", path, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// TestSelfRecordNarrativeRoundRecordsOnlyItsRealBlock is the measured
// reproduction, byte for byte from session 9adf61bc.
//
// RED on the position-blind extractor: it returned ONE note, and that note began
// with the sentence fragment "` 已经不是原文，而是被换成了…" instead of the round's
// real record.
// GREEN now: exactly one note, and it is the text the model wrote between the
// anchored delimiters.
func TestSelfRecordNarrativeRoundRecordsOnlyItsRealBlock(t *testing.T) {
	text := selfRecordFixtureText(t, narrativeRoundFixture)

	// The fixture must still BE the shape this test is about, or the arms below
	// prove nothing: an open marker that is NOT at the start of its line exists,
	// and so does a line-anchored one.
	if n := selfRecordInlineMarkerCount(text, selfRecordOpenMarker); n == 0 {
		t.Fatalf("%s carries no mid-sentence %s any more — the fixture no longer "+
			"reproduces the defect", narrativeRoundFixture, selfRecordOpenMarker)
	}

	notes := extractSelfNotes(text)
	if len(notes) != 1 {
		t.Fatalf("extractSelfNotes returned %d note(s) from a round with ONE real block: %#v",
			len(notes), notes)
	}
	got := notes[0]
	// The REAL note legitimately NAMES the marker inside its own sentence (this
	// measured round does exactly that: "第一轮的 [IMPORTANT] 在第二轮请求里以…").
	// So the assertion is not "the body is free of delimiters" — it is that the
	// body does not BEGIN at one, which is the whole difference between a record
	// and a fragment.
	if strings.HasPrefix(got, selfRecordOpenMarker) || strings.HasPrefix(got, selfRecordCloseMarker) {
		t.Fatalf("the recorded note still starts at a delimiter: %q", got)
	}
	// The fragment shipped by the old extractor STARTED here; the real block does
	// not. This is the assertion the whole epic reduces to.
	if strings.HasPrefix(got, "` 已经不是原文") {
		t.Fatalf("the note is the sentence fragment the marker cut out of the body: %q", got)
	}
	if !strings.HasPrefix(got, "核实了 turn-fold 与 compaction") {
		t.Fatalf("the note is not the round's real record:\n got %q", got)
	}
	if !strings.Contains(got, "assistant 消息不是第二来源") {
		t.Fatalf("the note lost its finding half — the block was cut short: %q", got)
	}
}

// TestSelfRecordQuotedBlockBeforeTheRealOne pins the case the epic names: the
// body quotes a COMPLETE block (both markers, mid-sentence), then gives the real
// one. Only the real one may be recorded — the quote is prose about the contract.
func TestSelfRecordQuotedBlockBeforeTheRealOne(t *testing.T) {
	text := "协议要求这样写：" + selfRecordOpenMarker + " x " + selfRecordCloseMarker + "。\n\n" +
		selfRecordOpenMarker + "\n本轮：修好了行首锚定。\n" + selfRecordCloseMarker + "\n"

	notes := extractSelfNotes(text)
	if len(notes) != 1 {
		t.Fatalf("extractSelfNotes returned %d note(s), want exactly the real block: %#v", len(notes), notes)
	}
	if notes[0] != "本轮：修好了行首锚定。" {
		t.Fatalf("the recorded note is %q, want the anchored block's body", notes[0])
	}
}

// TestSelfRecordInlineCloserDoesNotEndTheBlock is the pairing half: a mention of
// the CLOSE marker inside the block's own body must not close it early. The
// measured round #225 is exactly this shape (its body quotes both delimiters).
func TestSelfRecordInlineCloserDoesNotEndTheBlock(t *testing.T) {
	text := selfRecordOpenMarker + "\n上一轮我把 " + selfRecordCloseMarker + " 写在了行中间，所以没被认成块。\n" +
		selfRecordCloseMarker + "\n"

	notes := extractSelfNotes(text)
	if len(notes) != 1 {
		t.Fatalf("extractSelfNotes returned %d note(s), want 1: %#v", len(notes), notes)
	}
	if !strings.Contains(notes[0], "写在了行中间") {
		t.Fatalf("the block was closed at the inline mention, body = %q", notes[0])
	}
	if strings.HasSuffix(notes[0], "所以没被认成块。") == false {
		t.Fatalf("the block's body was cut short: %q", notes[0])
	}
}

// TestSelfRecordFragmentRoundRecordsOnlyItsRealBlock is the second measured
// round: two inline mentions, one anchored block, one note.
func TestSelfRecordFragmentRoundRecordsOnlyItsRealBlock(t *testing.T) {
	text := selfRecordFixtureText(t, fragmentRoundFixture)
	notes := extractSelfNotes(text)
	if len(notes) != 1 {
		t.Fatalf("extractSelfNotes returned %d note(s) from %s, want 1: %#v",
			len(notes), fragmentRoundFixture, notes)
	}
	if strings.HasPrefix(notes[0], "` 正文。") {
		t.Fatalf("the note is the sentence fragment the marker cut out of the body: %q", notes[0])
	}
}

// TestSelfRecordMentionOnlyRoundRecordsNothing: a round that TALKS about the
// markers (or quotes them) but writes no anchored block recorded NOTHING. The old
// extractor wrote a fragment for it — that is the pollution, and "no block" is the
// honest answer, not "a guess at where one might have been".
func TestSelfRecordMentionOnlyRoundRecordsNothing(t *testing.T) {
	text := "规则要求以 " + selfRecordOpenMarker + " 开头，我上一轮却把 " +
		selfRecordCloseMarker + " 落在了行尾，于是 " + selfRecordOpenMarker + " 被切在了句子中间。"

	if notes := extractSelfNotes(text); len(notes) != 0 {
		t.Fatalf("a round about the contract recorded %d note(s): %#v", len(notes), notes)
	}
}

// TestSelfRecordIndentedBlockIsStillABlock: the rule allows the delimiter to be
// indented, so indentation before it is horizontal whitespace, not prose. A fix
// that demanded column zero would silently drop a formatted note.
func TestSelfRecordIndentedBlockIsStillABlock(t *testing.T) {
	text := "结论如下。\n\n\t" + selfRecordOpenMarker + "\n\t本轮：缩进也要认。\n\t" + selfRecordCloseMarker + "\n"

	notes := extractSelfNotes(text)
	if len(notes) != 1 {
		t.Fatalf("an indented block was not extracted: %#v", notes)
	}
	if notes[0] != "本轮：缩进也要认。" {
		t.Fatalf("the recorded note is %q", notes[0])
	}
}

// TestSelfRecordUnclosedAnchoredOpenIsNotANote keeps the pre-existing refusal: an
// anchored open with no anchored closer is a half-written block, and inventing one
// would put the harness's guess into the durable file.
func TestSelfRecordUnclosedAnchoredOpenIsNotANote(t *testing.T) {
	text := "正文。\n" + selfRecordOpenMarker + "\n这句话后面没有闭合标记。\n"
	if notes := extractSelfNotes(text); len(notes) != 0 {
		t.Fatalf("an unclosed block was extracted: %#v", notes)
	}
}

// TestSelfRecordExtractsEveryAnchoredBlockInOrder keeps the multi-note contract
// (one answer may record more than one block) after the position rule.
func TestSelfRecordExtractsEveryAnchoredBlockInOrder(t *testing.T) {
	text := "前。\n" + selfRecordOpenMarker + "\n第一条。\n" + selfRecordCloseMarker + "\n中间提到 " +
		selfRecordOpenMarker + " 一下。\n" + selfRecordOpenMarker + "\n第二条。\n" + selfRecordCloseMarker + "\n"

	notes := extractSelfNotes(text)
	if len(notes) != 2 || notes[0] != "第一条。" || notes[1] != "第二条。" {
		t.Fatalf("notes = %#v, want the two anchored blocks in order", notes)
	}
}

// selfRecordInlineMarkerCount counts the occurrences of marker that are NOT
// line-anchored — i.e. the mentions. It is the TEST's own reading of the rule
// (deliberately not the production predicate) so the fixture guard cannot go
// green by agreeing with the code it is checking.
func selfRecordInlineMarkerCount(text, marker string) int {
	n := 0
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], marker)
		if i < 0 {
			return n
		}
		at := from + i
		lineStart := strings.LastIndexByte(text[:at], '\n') + 1
		if strings.TrimLeft(text[lineStart:at], " \t\r") != "" {
			n++
		}
		from = at + 1
	}
	return n
}
