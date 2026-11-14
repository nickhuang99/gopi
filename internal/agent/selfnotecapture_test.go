package agent

// selfnotecapture_test.go — the auto-memory capture gate (epic 自动记忆, item 3/5).
//
// WHAT THIS FILE PINS, in two halves:
//
//  1. THE CRITERION IS A SHAPE GATE, TABLE-DRIVEN. The operator's four content
//     forms each get a POSITIVE and a NEGATIVE example: a Chinese markdown table,
//     an ASCII topology diagram, a long conclusion, and a bare status report. The
//     negatives are the interesting half — they are notes that CLEAR the length
//     floor and still must not become memory, which is exactly the case a
//     "length only" gate would get wrong.
//  2. THE CAPTURE IS A STORE ON THE RESPONSE-END PATH THAT CHANGES NOTHING ELSE.
//     A real store lands a row with the session/round pointer; a run with no
//     session writes no anonymous row; a repeated note is stored once; a failed
//     store is not fatal; and `memoryStored` is NEVER set (the task-end memory
//     requirement stays the model's to satisfy).
//
// ISOLATION (Rule 32): the database is always testutil.NewTestDBPath(t), which
// pins GOPI_MEMORY_DB at a per-test file under the pinned scratch root and
// refuses a path inside the operator's live profile. Nothing here reads or writes
// ~/.gopi/memory.db.

import (
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// The criterion: one table, four content forms, ± examples.
// ---------------------------------------------------------------------------

// selfNoteCases is the table-driven criterion. Each row names the FORM under
// test, states WHY the verdict is what it is (so a later reader changing a
// constant sees the argument it was chosen for), and carries the note itself.
//
// The notes are built by a helper rather than pasted as 300-rune literals so the
// RULE they exercise is legible: `selfNoteProse(n, …)` produces exactly n runes
// of prose with no glyph and no pipe, which is what makes the length arms
// testable at their boundaries.
func selfNoteCases() []struct {
	name       string
	form       string
	note       string
	wantTier   string
	wantReason string
} {
	return []struct {
		name       string
		form       string
		note       string
		wantTier   string
		wantReason string
	}{
		{
			name: "table/positive: a Chinese markdown table IS a memory candidate",
			form: "中文 markdown 表格",
			note: selfNoteProse(selfNoteMinRunes, "合并前逐个核对了三处配置的默认值") +
				" | 配置项 | 默认值 | 判定 | | session_id | '' | 未记录 | | round_index | 0 | 无轮次 |",
			wantTier: selfNoteTierTable,
		},
		{
			name:       "table/negative: a single pipe in prose is not a table",
			form:       "中文 markdown 表格",
			note:       selfNoteProse(selfNoteMinRunes, "两种口径的取值分别是 A 与 B") + "。",
			wantReason: selfNoteRejectNoStructure,
		},
		{
			name: "topology/positive: an ASCII diagram IS the strongest candidate",
			form: "ASCII 拓扑图",
			note: selfNoteProse(selfNoteMinRunes, "本轮把调用链画清楚了") +
				" loop.go → selfnotecapture.go → memory.db(distilled_qa) ├─ origin 指针 └─ 去重表",
			wantTier: selfNoteTierTopology,
		},
		{
			name:       "topology/negative: talking ABOUT a diagram, in words, is not one",
			form:       "ASCII 拓扑图",
			note:       selfNoteProse(selfNoteMinRunes+50, "本轮把调用链的形状理清楚了但没有在正文里画出来") + "。",
			wantReason: selfNoteRejectNoStructure,
		},
		{
			name:     "conclusion/positive: a long written-out conclusion IS captured",
			form:     "大段总结",
			note:     selfNoteProse(selfNoteConclusionRunes, "抽取器把引用标记当成真笔记") + "。" + selfNoteFlagPhrase,
			wantTier: selfNoteTierConclusion,
		},
		{
			name:       "conclusion/negative: a statement that is neither shaped nor long",
			form:       "大段总结",
			note:       selfNoteProse(selfNoteConclusionRunes-60, "本轮把三处调用点都看过了没有发现新的缺陷") + "。",
			wantReason: selfNoteRejectNoStructure,
		},
		{
			name:       "conclusion/negative: length without reasoning is not a conclusion (long prose marker is absent)",
			form:       "大段总结",
			note:       selfNoteProse(selfNoteConclusionRunes+200, "两个字一组地重复堆出来的长句，没有任何说明与论断的措辞") + "。",
			wantReason: selfNoteRejectNoStructure,
		},
		{
			name:       "status-shell/negative: a bare status line is too short to be an experience",
			form:       "空壳状态报告",
			note:       "merge run 完成：三节点零运行，全绿，worktree 干净。",
			wantReason: selfNoteRejectTooShort,
		},
		{
			name:       "status-shell/negative: a LONG shell report is still shell, because its form says nothing",
			form:       "空壳状态报告",
			note:       selfNoteProse(selfNoteConclusionRunes+50, "状态报告：四个 worker 在跑，队列为空，没有失败行") + "。",
			wantReason: selfNoteRejectNoStructure,
		},
		{
			name: "status-shell/positive: the same status report WITH a node table is a candidate",
			form: "空壳状态报告",
			note: selfNoteProse(selfNoteMinRunes, "节点状态逐个记下来了") +
				" | 节点 | 角色 | 状态 | | zpeer | server | 忙 | | hp | client | 闲 |",
			wantTier: selfNoteTierTable,
		},
		{
			name:       "contamination: the contract's own placeholder is never stored",
			form:       "占位符",
			note:       selfRecordPlaceholderShape + selfRecordFindingSeparator + "<significant finding — optional>",
			wantReason: selfNoteRejectPlaceholder,
		},
		{
			name:       "contamination: a note that QUOTES the marker is a narrative, not a record",
			form:       "标记字面量",
			note:       selfNoteProse(selfNoteConclusionRunes+20, "本 run 没有合规的 "+selfNoteMarkerOpen+" 块") + "。" + selfNoteFlagPhrase,
			wantReason: selfNoteRejectMarker,
		},
	}
}

// TestClassifySelfNoteIsAShapeGate drives the criterion: every form gets a
// positive and a negative, and the tier names WHICH arm captured the note.
func TestClassifySelfNoteIsAShapeGate(t *testing.T) {
	forms := map[string]bool{}
	positives := map[string]bool{}
	for _, tc := range selfNoteCases() {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySelfNote(tc.note)
			if tc.wantTier != "" {
				if !got.Capture {
					t.Fatalf("(%s) refused as %q, want tier %q", tc.form, got.Reason, tc.wantTier)
				}
				if got.Tier != tc.wantTier {
					t.Fatalf("(%s) captured as tier %q, want %q — the wrong arm claims the note", tc.form, got.Tier, tc.wantTier)
				}
				if got.Reason != "" {
					t.Errorf("(%s) a captured note carries reason %q", tc.form, got.Reason)
				}
				return
			}
			if got.Capture {
				t.Fatalf("(%s) CAPTURED as %q, want refusal (%s) — a shell note must not become memory", tc.form, got.Tier, tc.wantReason)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("(%s) refused for %q, want %q", tc.form, got.Reason, tc.wantReason)
			}
			if got.Tier != "" {
				t.Errorf("(%s) a refused note carries tier %q", tc.form, got.Tier)
			}
		})
		if tc.wantTier != "" {
			positives[tc.form] = true
		}
		forms[tc.form] = true
	}

	// A table whose every row is a refusal would pass the loop above while
	// testing nothing about the gates. The floor is the four content forms the
	// operator named, EACH with a positive example.
	wantForms := []string{"中文 markdown 表格", "ASCII 拓扑图", "大段总结", "空壳状态报告"}
	for _, f := range wantForms {
		if !forms[f] {
			t.Errorf("no case exercises the form %q", f)
		}
		if !positives[f] {
			t.Errorf("form %q has no positive example — the table proves only that things are refused", f)
		}
	}
}

// TestClassifySelfNoteBoundaries pins the two length arms AT their boundaries, so
// a later edit to a constant cannot move the gate silently: one rune below is a
// refusal, at the constant is a capture (for the arm in question).
func TestClassifySelfNoteBoundaries(t *testing.T) {
	below := selfNoteProse(selfNoteMinRunes-1, "短笔记的下边界")
	if v := classifySelfNote(below); v.Capture || v.Reason != selfNoteRejectTooShort {
		t.Fatalf("%d runes (floor %d) classified %+v, want %s", selfNoteMinRunes-1, selfNoteMinRunes, v, selfNoteRejectTooShort)
	}
	atFloor := selfNoteProse(selfNoteMinRunes, "刚好越过下界的笔记")
	if v := classifySelfNote(atFloor); v.Capture {
		t.Fatalf("a %d-rune note with no form captured as %q — the floor is not a form", selfNoteMinRunes, v.Tier)
	} else if v.Reason != selfNoteRejectNoStructure {
		t.Fatalf("a %d-rune shapeless note refused for %q, want %q", selfNoteMinRunes, v.Reason, selfNoteRejectNoStructure)
	}

	flag := []rune(selfNoteFlagPhrase)
	justShort := selfNoteProse(selfNoteConclusionRunes-len(flag)-1, "段落还差一个字") + selfNoteFlagPhrase
	atConclusion := selfNoteProse(selfNoteConclusionRunes-len(flag), "段落到了下界") + selfNoteFlagPhrase
	if got := len([]rune(justShort)); got != selfNoteConclusionRunes-1 {
		t.Fatalf("fixture built %d runes, want %d", got, selfNoteConclusionRunes-1)
	}
	if got := len([]rune(atConclusion)); got != selfNoteConclusionRunes {
		t.Fatalf("fixture built %d runes, want %d", got, selfNoteConclusionRunes)
	}
	if v := classifySelfNote(justShort); v.Capture {
		t.Fatalf("%d runes (conclusion arm %d) captured as %q — the arm fires early", selfNoteConclusionRunes-1, selfNoteConclusionRunes, v.Tier)
	}
	if v := classifySelfNote(atConclusion); !v.Capture || v.Tier != selfNoteTierConclusion {
		t.Fatalf("%d runes classified %+v, want the %s arm", selfNoteConclusionRunes+len([]rune(selfNoteFlagPhrase)), v, selfNoteTierConclusion)
	}
}

// TestClassifySelfNotePrefersTheStrongestForm: the arms are a precedence ladder,
// not a set — a note that is BOTH long and drawn is recorded once, as a diagram.
func TestClassifySelfNotePrefersTheStrongestForm(t *testing.T) {
	note := selfNoteProse(selfNoteConclusionRunes+50, "这一轮同时很长并且画了图") + " a ├ b"
	if v := classifySelfNote(note); v.Tier != selfNoteTierTopology {
		t.Fatalf("a long, drawn note captured as %q — the ladder must let the diagram claim it", v.Tier)
	}
}

// ---------------------------------------------------------------------------
// The capture: a real store, a real pointer, and the properties the epic owes.
// ---------------------------------------------------------------------------

// newCaptureTestAgent builds a real Agent whose session log lives under an
// isolated GOPI_HOME and whose knowledge base is a per-test database (both
// pinned by the scaffold). Its round is set, because the pointer is what the
// capture is for.
func newCaptureTestAgent(t *testing.T, round int) *Agent {
	t.Helper()
	testutil.NewTestDBPath(t) // pins GOPI_MEMORY_DB at a fresh per-test ledger
	t.Setenv(config.GopiDirEnv, t.TempDir())
	sl, err := NewSessionLoggerForRun("capture-probe-model", filepath.Join(t.TempDir(), "raw.log"), "capture-probe-session", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	t.Cleanup(func() { _ = sl.Close() }) //stylegate:ignore cleanup close of the note-capture fixture's logger; the captured note is the assertion
	return &Agent{SessionLog: sl, hippocampusRound: round, Logger: log.New(io.Discard, "", 0)}
}

// capturedRow reads back the row a capture produced: the newest distilled_qa row
// whose question is the note. It fails when there is none — a "captured" claim
// with no row behind it is the assertion this whole file exists to make
// impossible.
// capturedRow finds the row a capture produced and returns its pointer fields.
//
// IT LOOKS THE ROW UP BY `answer`, NOT BY `question` (corrected 2026-11-02 with
// the retrieval-key fix). The row used to be found by question because the two
// columns held the same bytes; now the question is a derived key and the answer
// is the body, so the body is what identifies the row. It then ASSERTS the key's
// own contract — non-empty, bounded, and not a second copy of the body — so a
// regression that put the body back into the question field fails HERE rather
// than silently shipping (the defect this pair of columns produced measured
// 2284/2284 and 8041/8043 rows with question == answer).
func capturedRow(t *testing.T, note string) (id int, session string, round int, source string) {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(testDBPathFromEnv(t)))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var question, answer string
	row := db.QueryRow("SELECT id, session_id, round_index, sources, question, answer FROM distilled_qa WHERE answer = ? ORDER BY id DESC", note)
	if err := row.Scan(&id, &session, &round, &source, &question, &answer); err != nil {
		t.Fatalf("no distilled_qa row for the captured note: %v", err)
	}
	if answer != note {
		t.Errorf("the stored answer lost bytes: got %d runes, want %d", utf8.RuneCountInString(answer), utf8.RuneCountInString(note))
	}
	if question == "" {
		t.Error("the stored question is EMPTY — memory search would have nothing to rank against")
	}
	if utf8.RuneCountInString(question) > memoryQuestionKeyRunes {
		t.Errorf("the stored question is %d runes, over the %d bound: %q",
			utf8.RuneCountInString(question), memoryQuestionKeyRunes, question)
	}
	if question == answer {
		t.Errorf("the stored question is byte-identical to the answer — the retrieval-key defect is back")
	}
	return id, session, round, source
}

// testDBPathFromEnv reads back the ledger path the scaffold pinned. It is a
// reader only — the pin itself is NewTestDBPath's job (Rule 32: never hand-roll).
func testDBPathFromEnv(t *testing.T) string {
	t.Helper()
	p, _ := config.ResolveLedger("")
	if strings.TrimSpace(p) == "" {
		t.Fatal("the test ledger path is unset — NewTestDBPath must pin it before any store")
	}
	return p
}

// TestCaptureSelfNotesStoresACandidateWithItsPointer is the end-to-end half of
// the criterion: a shaped note becomes a row, and the row carries the session and
// the round of the run that produced it (epic item 2/5's pointer).
func TestCaptureSelfNotesStoresACandidateWithItsPointer(t *testing.T) {
	ag := newCaptureTestAgent(t, 3)
	note := selfNoteProse(selfNoteConclusionRunes, "捕获这一轮的结论段并带上指针") + "。"

	stored := ag.captureSelfNotes([]string{note})
	if stored != 1 {
		t.Fatalf("captureSelfNotes stored %d note(s), want 1", stored)
	}
	id, session, round, source := capturedRow(t, note)
	if id <= 0 {
		t.Fatal("row id must be a real row id")
	}
	if session != "capture-probe-session" {
		t.Errorf("session_id = %q, want the run's own session", session)
	}
	if round != 3 {
		t.Errorf("round_index = %d, want the run's own round 3", round)
	}
	if source != selfNoteMemorySource {
		t.Errorf("sources = %q, want the funnel's own class %q", source, selfNoteMemorySource)
	}
	t.Logf("captured row id=%d session=%s round=%d", id, session, round)
}

// TestCaptureSelfNotesIsLocalAndIdempotent: the second round that repeats the
// same note pays one store, not two. The store's own dedup would also collapse
// it — this asserts the LOCAL guard, which is the cheaper of the two.
func TestCaptureSelfNotesIsLocalAndIdempotent(t *testing.T) {
	ag := newCaptureTestAgent(t, 2)
	note := selfNoteProse(selfNoteConclusionRunes, "同一条笔记在同一 run 内重复出现") + "。" + selfNoteFlagPhrase

	if got := ag.captureSelfNotes([]string{note}); got != 1 {
		t.Fatalf("first pass stored %d, want 1", got)
	}
	if got := ag.captureSelfNotes([]string{note}); got != 0 {
		t.Fatalf("second pass stored %d, want 0 — the run-local dedup did not fire", got)
	}
}

// TestCaptureSelfNotesNeverSatisfiesTheMemoryRequirement is the epic's explicit
// constraint: the harness's own capture must not set a.memoryStored, or the
// task-end checkpoint that makes the MODEL store a lesson would be silently
// retired.
func TestCaptureSelfNotesNeverSatisfiesTheMemoryRequirement(t *testing.T) {
	ag := newCaptureTestAgent(t, 1)
	if ag.memoryStored {
		t.Fatal("fixture starts with memoryStored set")
	}
	note := selfNoteProse(selfNoteConclusionRunes, "自动捕获不得替模型满足记忆要求") + "。" + selfNoteFlagPhrase
	if stored := ag.captureSelfNotes([]string{note}); stored != 1 {
		t.Fatalf("capture stored %d, want 1 (the test must exercise a REAL capture)", stored)
	}
	if ag.memoryStored {
		t.Fatal("captureSelfNotes set memoryStored — the task-end memory requirement would never fire again")
	}
}

// TestCaptureSelfNotesRefusesASessionlessRun: with no session there is no pointer
// to stamp, and the capture writes NO anonymous row — it reports and moves on.
// (This is the shape memory_origin_stamp.go refuses; the capture never
// manufactures it.)
func TestCaptureSelfNotesRefusesASessionlessRun(t *testing.T) {
	testutil.NewTestDBPath(t)
	ag := &Agent{hippocampusRound: 4, Logger: log.New(io.Discard, "", 0)}
	note := selfNoteProse(selfNoteConclusionRunes, "没有 session 的运行不应写入匿名行") + "。" + selfNoteFlagPhrase

	if stored := ag.captureSelfNotes([]string{note}); stored != 0 {
		t.Fatalf("a session-less run stored %d note(s), want 0", stored)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(testDBPathFromEnv(t)))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='distilled_qa'").Scan(&n); err != nil {
		t.Fatalf("probe schema: %v", err)
	}
	if n == 0 {
		return // nothing was ever opened, which is also "no anonymous row"
	}
	var rows int
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa WHERE sources = ?", selfNoteMemorySource).Scan(&rows); err != nil {
		t.Fatalf("probe rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("%d anonymous row(s) were written for a run with no session to point at", rows)
	}
}

// TestCaptureSelfNotesRefusesUnshapedNotes end-to-end: the refractory arm reaches
// the database as a non-event — a short status note is recorded in the side
// channel and stores nothing.
func TestCaptureSelfNotesRefusesUnshapedNotes(t *testing.T) {
	ag := newCaptureTestAgent(t, 1)
	short := "merge run 完成：三节点零运行，全绿。"
	shapeless := selfNoteProse(selfNoteConclusionRunes+40, "空壳状态报告：队列为空没有任何失败行") + "。"

	if got := ag.captureSelfNotes([]string{short, shapeless}); got != 0 {
		t.Fatalf("capture stored %d unshaped note(s), want 0", got)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(testDBPathFromEnv(t)))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&rows); err != nil {
		// A store that never ran never created the table: the refusal held.
		return
	}
	if rows != 0 {
		t.Fatalf("distilled_qa holds %d row(s) after a round of unshaped notes", rows)
	}
}

// selfNoteProse builds a shapeless prose body of EXACTLY want runes: it repeats
// seed until the length is reached. The function is the test's own unit of
// measurement for the length arms, so a boundary test cannot drift with a pasted
// literal's invisible length.
// selfNoteFlagPhrase is the reasoning marker the positive conclusion cases append,
// so a fixture can be long AND say how it knows without repeating the marker list
// here (Rule 14: the vocabulary lives in selfNoteSummaryMarkers, the test reads a
// phrase from it).
const selfNoteFlagPhrase = "判定：这与实测一致。"

func selfNoteProse(want int, seed string) string {
	if seed == "" {
		seed = "无"
	}
	runes := []rune(seed)
	out := make([]rune, 0, want)
	for len(out) < want {
		out = append(out, runes...)
	}
	return string(out[:want])
}
