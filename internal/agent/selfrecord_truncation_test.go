package agent

// selfrecord_truncation_test.go — the CUT NOTE SAYS IT WAS CUT, and says how much.
//
// The defect this pins (measured on this host, 2026-11-10): the cap appended a
// bare "…", which is exactly what an author types to mean "and so on". Of 5,918
// recorded note lines on this host, ONE had actually been cut, and 1,373 END
// with an author-written "…" — so the mark a reader had to notice was
// indistinguishable from the text's own punctuation, at a ratio of 1:1,373.
//
// Three things must hold, and each is a separate arm below:
//
//  1. NOTHING IS REWRITTEN UNDER THE CAP — a note that fits is returned
//     byte-identical, so this change is invisible to every existing note.
//  2. A CUT NOTE STILL FITS THE CAP. The tail is inside
//     maxSelfRecordNoteChars, not beside it: turnfold.go derives
//     turnFoldNoteBodyBudget = maxSelfRecordNoteChars × turnFoldBodyNotes, and a
//     note of 500 runes PLUS a tail would spend budget that arithmetic believes
//     is reserved.
//  3. THE STATED LOSS IS THE REAL LOSS, in runes, and the cut lands on a rune
//     boundary (a CJK note must never be split mid-character). The line is the
//     durable half of the report; the run log is the volatile half, and both are
//     checked.

import (
	"bytes"
	"log"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// realCutNoteBody is the note from session 06cbdee7-ec6f-4f9f-b9a5-bd975028decb,
// self-record.md line 10, read off this host's disk: the ONE genuinely cut note
// the epic found. It is 501 runes, so the cap dropped exactly 1 rune of it and
// the OLD tail then made the line 505 runes — the arithmetic the fix has to keep
// honest in both directions.
const realCutNoteBody = "` 还会是 `r<n>`，从下一轮开始就变成 `t<n>`。想立刻验证的话，退出后重开一个 chat 即可。 **turn 的语义，与你说的一致**：代码里的判据只有一个，即 `sessionevent.IsTurnOpeningRow`；一次用户输入（不管是人敲的、`gopi notify` 投递的，还是 surface 自己用 `chat_surface type` 输入的）开启一个 turn，中间的每一次工具调用 HTTP 请求与响应都算这个 turn 里的 round。所以 turn 不等于 round。`gopi session log --turns` 现在打的就是这个轴，本会话实测 `turns = 11`（人打的 6 个，其余由 notify/self-type 开启），每行的 ROUNDS 列就是该 turn 里发生了几次请求。 **另外发现一个异常**：我刚跑 `gopi session log ... --turns` 时，**进程输出正确，但退出码是 1**。这不是命令写错——同样的命令带着输出打完了整张表。按规矩我不在控制台里修它，直接提了 bug 行记录。这…"

// realCutNoteRunes is measured, not asserted: the fixture's own length is the
// input to the arithmetic below.
func realCutNoteRunes(t *testing.T) int {
	t.Helper()
	return utf8.RuneCountInString(realCutNoteBody)
}

// TestSelfRecordNoteUnderTheCapIsVerbatim pins arm 1: a note that fits is
// returned unchanged — no tail, no normalisation beyond the ONE flattening the
// layer always did, and a loss count of zero.
func TestSelfRecordNoteUnderTheCapIsVerbatim(t *testing.T) {
	exact := strings.Repeat("知", maxSelfRecordNoteChars) // exactly the cap
	for _, note := range []string{
		"short note",
		"  spaced   out\ttabs\nand a newline  ", // flattened, then compared
		exact,
	} {
		got, dropped := collapseSelfNoteCount(note)
		if dropped != 0 {
			t.Fatalf("note of %d runes reported %d dropped, want 0", utf8.RuneCountInString(note), dropped)
		}
		want := strings.Join(strings.Fields(note), " ")
		if got != want {
			t.Fatalf("a note within the cap was rewritten:\n got %q\nwant %q", got, want)
		}
		if strings.Contains(got, "truncated") {
			t.Fatalf("a note within the cap carries a truncation mark: %q", got)
		}
		if collapseSelfNote(note) != got {
			t.Fatalf("collapseSelfNote disagrees with collapseSelfNoteCount on %q", note)
		}
	}
}

// TestSelfRecordCutNoteFitsTheCapAndStatesTheLoss pins arms 2 and 3 together on
// shapes that exercise both digit widths of the count: one rune over the cap,
// and a note long enough that the count needs another digit (the case a naive
// "keep 500 - len(tail)" implementation gets wrong by one).
func TestSelfRecordCutNoteFitsTheCapAndStatesTheLoss(t *testing.T) {
	for _, over := range []int{1, 2, 9, 10, 11, 99, 100, 101, 1373, 4000} {
		note := strings.Repeat("知", maxSelfRecordNoteChars+over)
		got, dropped := collapseSelfNoteCount(note)

		total := utf8.RuneCountInString(got)
		if total > maxSelfRecordNoteChars {
			t.Fatalf("over=%d: the cut note is %d runes, over the %d cap", over, total, maxSelfRecordNoteChars)
		}
		// The count is the REAL loss, not the overshoot: the tail costs runes out
		// of the kept body too ("…[truncated N chars]" is ~22 runes), so dropped =
		// flat runes − kept runes. Asserting "dropped == over" would pin the tail
		// to zero width — the arithmetic that makes the mark free, and it is not.
		if want := utf8.RuneCountInString(note) - utf8.RuneCountInString(strings.TrimSuffix(got, selfRecordTruncationMark(dropped))); dropped != want {
			t.Fatalf("over=%d: reported %d dropped, want %d (flat − kept)", over, dropped, want)
		}
		// The mark is IN the note, states the count, and is not the author's "…".
		mark := selfRecordTruncationMark(dropped)
		if !strings.HasSuffix(got, mark) {
			t.Fatalf("over=%d: the note does not end with %q:\n%s", over, mark, truncationTestTail(got))
		}
		if strings.Contains(got, "[truncated 0 chars]") {
			t.Fatalf("over=%d: a cut note claimed zero loss: %q", over, got)
		}
		// CONSERVATION: the kept body plus the dropped runes IS the original — the
		// tail is extra, which is why it comes out of the cap and not out of thin
		// air. And the whole recorded note stays inside the cap.
		body := strings.TrimSuffix(got, mark)
		if n := utf8.RuneCountInString(body) + dropped; n != utf8.RuneCountInString(note) {
			t.Fatalf("over=%d: kept(%d) + dropped(%d) = %d, want %d",
				over, utf8.RuneCountInString(body), dropped, n, utf8.RuneCountInString(note))
		}
		if n := utf8.RuneCountInString(body) + utf8.RuneCountInString(mark); n != total {
			t.Fatalf("over=%d: kept(%d) + tail(%d) = %d, but the note is %d", over, utf8.RuneCountInString(body), utf8.RuneCountInString(mark), n, total)
		}
		// Rune boundary: the tail must be valid UTF-8 with no replacement char.
		if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
			t.Fatalf("over=%d: the cut split a rune: %q", over, got)
		}
		if !strings.HasPrefix(got, strings.Repeat("知", 20)) {
			t.Fatalf("over=%d: the kept head is not the original text: %.40s", over, got)
		}
	}
}

// TestSelfRecordTruncationIsReportedOnTheRunLog pins the Rule 15 half that does
// not live in the file: the run's own log says a note was cut and by how much,
// with the SAME number the tail states. A run with no logger still records the
// note (the tail is the durable report) and must not panic.
func TestSelfRecordTruncationIsReportedOnTheRunLog(t *testing.T) {
	var buf bytes.Buffer
	ag := &Agent{Logger: log.New(&buf, "", 0), Ctx: &Context{}}
	const over = 37
	note := strings.Repeat("满", maxSelfRecordNoteChars+over)
	// The count the note will state, computed by the SAME function the writer
	// uses — then the LOG is checked against it. Hard-coding "37" would be
	// pinning the tail's width to zero, which it never is.
	want, wantDropped := collapseSelfNoteCount(note)
	if !strings.HasSuffix(want, selfRecordTruncationMark(wantDropped)) {
		t.Fatalf("fixture did not cut: %q", truncationTestTail(want))
	}
	recorded := ag.recordSelfNotes(selfRecordOpenMarker+"\n"+note+"\n"+selfRecordCloseMarker, "")
	if len(recorded) != 1 {
		t.Fatalf("recordSelfNotes returned %d note(s), want 1", len(recorded))
	}
	if recorded[0] != want {
		t.Fatalf("the recorded note differs from the collapser's answer:\n got %q\nwant %q", truncationTestTail(recorded[0]), truncationTestTail(want))
	}
	logged := buf.String()
	if !strings.Contains(logged, "self-record: note cut") {
		t.Fatalf("the run log did not report the cut:\n%s", logged)
	}
	if !strings.Contains(logged, strconv.Itoa(wantDropped)+" char(s) dropped") {
		t.Fatalf("the run log did not state the %d dropped chars:\n%s", wantDropped, logged)
	}
	if !strings.Contains(logged, selfRecordTruncationMark(wantDropped)) {
		t.Fatalf("the run log's tail does not match the note's:\n%s", logged)
	}

	// No logger: no panic, and the note is still recorded with its mark.
	silent := &Agent{Ctx: &Context{}}
	got := silent.recordSelfNotes(selfRecordOpenMarker+"\n"+note+"\n"+selfRecordCloseMarker, "")
	if len(got) != 1 || !strings.HasSuffix(got[0], selfRecordTruncationMark(wantDropped)) {
		t.Fatalf("a run with no logger lost the cut note: %v", got)
	}
}

// TestSelfRecordCutNoteSurvivesTheLineParser pins that the mark changes NOTHING
// for readers: the ONE parser still returns the stamp number and the note body,
// so a marked line is indexed, re-injected and folded exactly like any other.
// The parser is deliberately untouched by this change (no shape moved), and this
// arm is what proves it.
func TestSelfRecordCutNoteSurvivesTheLineParser(t *testing.T) {
	cut, dropped := collapseSelfNoteCount(strings.Repeat("知", maxSelfRecordNoteChars+11))
	if dropped <= 11 {
		t.Fatalf("fixture dropped %d runes, want at least the 11 overshoot (the tail costs more)", dropped)
	}
	mark := selfRecordTruncationMark(dropped)
	for _, stamp := range []string{
		selfRecordLinePrefix(7), // `t7 `
		"r9 ",                   // the retired round stamp, still read
		selfRecordLegacyLineMark,
	} {
		line := stamp + cut
		n, note, ok := selfRecordNoteFromLine(line)
		if !ok {
			t.Fatalf("the parser refused a stamped cut note: %q", line)
		}
		if !strings.Contains(note, mark) {
			t.Fatalf("the parser dropped the mark from %q:\n%.120s", stamp, note)
		}
		if utf8.RuneCountInString(note) > maxSelfRecordNoteChars {
			t.Fatalf("the parsed note is %d runes, over the cap", utf8.RuneCountInString(note))
		}
		want := map[string]int{selfRecordLinePrefix(7): 7, "r9 ": 9, selfRecordLegacyLineMark: 0}[stamp]
		if n != want {
			t.Fatalf("stamp %q parsed to %d, want %d", stamp, n, want)
		}
	}
}

// TestSelfRecordRealCutLineFromSession06cbdee7 runs the fix against the ONE real
// cut note this epic found, read off this host's disk (session
// 06cbdee7-ec6f-4f9f-b9a5-bd975028decb, line 10 — 505 runes as written, of which
// the old tail "…" was the last one). The claims are the acceptance criteria,
// measured on the real text rather than on a synthetic string.
func TestSelfRecordRealCutLineFromSession06cbdee7(t *testing.T) {
	in := realCutNoteRunes(t)
	got, dropped := collapseSelfNoteCount(realCutNoteBody)
	total := utf8.RuneCountInString(got)

	if total > maxSelfRecordNoteChars {
		t.Fatalf("the real note collapsed to %d runes, over the %d cap", total, maxSelfRecordNoteChars)
	}
	// CONSERVATION on the REAL text: what the note keeps, plus what it says it
	// dropped, is the input — no rune may vanish unaccounted for, which is the
	// whole point of stating a number instead of an ellipsis.
	body := strings.TrimSuffix(got, selfRecordTruncationMark(dropped))
	if n := utf8.RuneCountInString(body) + dropped; n != in {
		t.Fatalf("the real note lost runes to the arithmetic: %d kept + %d dropped = %d, input %d", utf8.RuneCountInString(body), dropped, n, in)
	}
	if dropped <= 0 {
		t.Fatalf("the real 505-rune line was not cut at all (input %d runes)", in)
	}
	if !strings.HasSuffix(got, selfRecordTruncationMark(dropped)) {
		t.Fatalf("the real note does not end with its loss statement:\n%.100s", got)
	}
	if !strings.Contains(got, "[truncated "+strconv.Itoa(dropped)+" chars]") {
		t.Fatalf("the real note's mark does not state %d chars:\n%.100s", dropped, got)
	}
	// The mark must be distinguishable from the author's own ellipsis: the old
	// tail was a bare "…", and the real text ends with one. Comparing the two
	// strings is the whole point of the fix, so assert it directly.
	if selfRecordTruncationMark(dropped) == "…" || strings.HasSuffix(got, "…") {
		t.Fatalf("the cut tail is still indistinguishable from an author ellipsis: %q", truncationTestTail(got))
	}
	if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatal("the real note split a rune")
	}
	t.Logf("real note: input %d runes → recorded %d runes, dropped %d, tail %q",
		in, total, dropped, selfRecordTruncationMark(dropped))
}

// truncationTestTail returns the last truncationTestTailRunes runes of a string,
// so a failure message shows the END of a long note (where the mark lives)
// without dumping 500 runes of CJK into the test log.
func truncationTestTail(s string) string {
	r := []rune(s)
	if len(r) <= truncationTestTailRunes {
		return s
	}
	return string(r[len(r)-truncationTestTailRunes:])
}

// truncationTestTailRunes is how much of a failure's note to print.
const truncationTestTailRunes = 60
