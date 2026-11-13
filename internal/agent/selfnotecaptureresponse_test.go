package agent

// selfnotecaptureresponse_test.go — the response shape gate's own proofs.
//
// THE SHAPE OF THIS FILE. Every case is LITERAL-DRIVEN (the epic's acceptance
// clause): one hand-written body per arm, so a threshold change fails against a
// text a reader can see, rather than against a corpus that would silently move
// with it. The corpus measurement lives in exactly ONE place — the volcengine
// regression below, which reads the operator's real billing round off disk — and
// the numbers those literals were calibrated from are quoted in
// internal/agent/selfnotecaptureresponse.go beside each constant.
//
// EVERY ARM HAS A RED PROOF, and the red case is either the NOTE gate's rule on
// this corpus (the rule that was wrong here) or the pre-fix response behaviour.
// A green-only test would prove nothing about a gate whose whole point is to
// refuse things (Rule 10.2: the red case is the instrument).

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// respGateTestRune is a filler rune that carries NO glyph of any arm: no
	// box-drawing character, no markdown pipe, no marker from
	// selfNoteSummaryMarkers. A body built from it is shapeless BY
	// CONSTRUCTION, so a test's length arithmetic does not accidentally cross
	// another arm's threshold.
	respGateTestRune = "甲"

	// respGateTestTableRow is ONE markdown table row with its separator: the
	// smallest thing that is a table. It is written out here rather than
	// derived so the red/green pair below differs in exactly one row.
	respGateTestTableRow = "\n| a | b |\n|---|---|\n| 1 | 2 |\n"

	// respGateTestBoxDiagram is a body fragment carrying box-drawing glyphs —
	// the diagram shape as the console actually renders it.
	respGateTestBoxDiagram = "\n┌───────┐\n│ boxed │\n└───────┘\n"

	// respGateTestArrowProse is the measured FALSE POSITIVE of the note gate's
	// rule on responses: `→` as ordinary prose punctuation. 155 of the 268
	// measured bodies carry at least one.
	respGateTestArrowProse = "\nA 修好了 → B 还可以用，client → server 一次往返。\n"
)

// respGateFiller returns n copies of the neutral rune as a body.
func respGateFiller(n int) string {
	return strings.Repeat(respGateTestRune, n)
}

// respGateBodyOf builds a body of exactly want runes around a fragment: filler
// on both sides, so the fragment's own glyphs are the only shape present.
func respGateBodyOf(t *testing.T, want int, fragment string) string {
	t.Helper()
	inner := len([]rune(fragment))
	if want < inner {
		t.Fatalf("want %d runes, fragment alone is %d", want, inner)
	}
	head := (want - inner) / 2
	tail := want - inner - head
	return respGateFiller(head) + fragment + respGateFiller(tail)
}

// --- the arms, one literal each ----------------------------------------------

// TestResponseGateCapturesEachArmFromALiteral is the property test the epic asks
// for: each capturing arm is reached by a body that carries THAT shape and
// nothing else, at a length just past the shared floor.
func TestResponseGateCapturesEachArmFromALiteral(t *testing.T) {
	const at = respCaptureMinRunes + 1

	cases := []struct {
		arm    string
		body   string
		branch string
	}{
		{
			arm:    respCaptureTierTopology,
			body:   respGateBodyOf(t, at, respGateTestBoxDiagram),
			branch: "a rendered box diagram (>= respCaptureTopologyMin box glyphs)",
		},
		{
			arm:    respCaptureTierTable,
			body:   respGateBodyOf(t, at, respGateTestTableRow),
			branch: "one markdown table (>= respCaptureTableRowMin separator row)",
		},
		{
			arm:    respCaptureTierConclusion,
			body:   respGateBodyOf(t, respCaptureConclusionRunes+1, "结论：实测如此。"),
			branch: "past respCaptureConclusionRunes AND carrying a reasoning marker",
		},
	}
	for _, c := range cases {
		t.Run(c.arm, func(t *testing.T) {
			v := classifyResponseText(c.body)
			if !v.Capture {
				t.Fatalf("the %s arm did not fire on %s (refused: %s)",
					c.arm, c.branch, v.Reason)
			}
			if v.Tier != c.arm {
				t.Fatalf("body of %d runes classified as %q, want %q — the arms are "+
					"ordered (topology > table > conclusion) and this literal carries only %s",
					len([]rune(c.body)), v.Tier, c.arm, c.branch)
			}
		})
	}
}

// TestResponseGateRefusesWhatItShouldNotKeep pins each refusal branch: the two
// buckets (too-short, no-structure) plus the note-only case. A gate that
// captured everything would satisfy the arm test above and be useless.
func TestResponseGateRefusesWhatItShouldNotKeep(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		reason string
	}{
		{
			name:   "an acknowledgement",
			body:   "收到，这就去办。",
			reason: selfNoteRejectTooShort,
		},
		{
			name:   "long but shapeless prose",
			body:   respGateFiller(respCaptureMinRunes + 1),
			reason: selfNoteRejectNoStructure,
		},
		{
			name:   "nothing but the round's own note",
			body:   selfNoteMarkerOpen + "\n本轮：查了两处代码。\n" + selfNoteMarkerClose,
			reason: respCaptureNoteOnly,
		},
		{
			name:   "a long note is still only a note",
			body:   selfNoteMarkerOpen + respGateFiller(respCaptureConclusionRunes+10) + selfNoteMarkerClose,
			reason: respCaptureNoteOnly,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := classifyResponseText(c.body)
			if v.Capture {
				t.Fatalf("%q was captured as %q — this body must not become a row", c.name, v.Tier)
			}
			if v.Reason != c.reason {
				t.Fatalf("%q refused as %q, want %q", c.name, v.Reason, c.reason)
			}
		})
	}
}

// --- the red proofs -----------------------------------------------------------

// TestResponseTableArmRequiresAMarkdownSeparatorRow is the red proof for the
// TABLE arm — and the red case is the NOTE gate's own rule, applied here.
//
// The note gate counts pipes (`strings.Count(note, "|") >= 3`,
// selfNoteTablePipeMin). On the note corpus that is right: a note has no room
// for a table, so pipes mean "the model drew something". On responses it is a
// false-positive machine — 5 of the 268 measured bodies carry three pipes with
// no table at all, and the response that started this epic (the model explaining
// in PROSE that the operator's table had died) is one of them. The green case is
// the operator's actual table.
func TestResponseTableArmRequiresAMarkdownSeparatorRow(t *testing.T) {
	// RED: three pipes, no separator row — the note gate's rule would capture it.
	prose := "他问 `| 产品 | 计费单元 | 单价 |` 这张表去哪了，我答：只躺在正文里就不进闸门。"
	red := respGateBodyOf(t, respCaptureMinRunes+1, "\n"+prose+"\n")
	if strings.Count(red, "|") < selfNoteTablePipeMin {
		t.Fatalf("the red fixture carries %d pipes; the note gate's rule needs >= %d to fire, "+
			"so this case would prove nothing about the difference",
			strings.Count(red, "|"), selfNoteTablePipeMin)
	}
	if v := classifySelfNote(red); !v.Capture || v.Tier != respCaptureTierTable {
		t.Fatalf("the NOTE gate should be the red case here — it classified the pipe-quoting prose "+
			"as captured=%v tier=%q; if the note rule changed, re-derive this proof", v.Capture, v.Tier)
	}
	if v := classifyResponseText(red); v.Capture {
		t.Fatalf("the response gate captured pipe-quoting PROSE as %q — the table arm must "+
			"require a separator row, not a pipe count", v.Tier)
	}

	// GREEN: one separator row makes it a table.
	green := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)
	v := classifyResponseText(green)
	if !v.Capture || v.Tier != respCaptureTierTable {
		t.Fatalf("a body carrying a markdown separator row was captured=%v tier=%q", v.Capture, v.Tier)
	}
}

// TestResponseTopologyArmIgnoresArrows is the red proof for the TOPOLOGY arm's
// glyph set, and it is the single largest calibration decision in this gate.
//
// The note gate treats ANY glyph in selfNoteTopologyGlyphs — arrows included —
// as a diagram. On responses the arrow is prose punctuation: 155 of the 268
// measured bodies contain `→`, versus 14 containing box-drawing characters. The
// red case below is what the arrow-inclusive rule would have produced; the green
// case is the box diagram.
func TestResponseTopologyArmIgnoresArrows(t *testing.T) {
	// The red case AS THE NOTE GATE SEES IT: the arrows are a diagram there.
	arrowBody := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestArrowProse)
	if !strings.ContainsAny(arrowBody, selfNoteTopologyGlyphs) {
		t.Fatalf("the fixture carries no glyph of selfNoteTopologyGlyphs, so it cannot stand " +
			"for the false positive this test exists for")
	}
	if v := classifySelfNote(arrowBody); !v.Capture || v.Tier != respCaptureTierTopology {
		t.Fatalf("the NOTE gate should read prose arrows as topology — it answered captured=%v tier=%q",
			v.Capture, v.Tier)
	}
	// ...and the response gate must NOT: this is the whole difference.
	if v := classifyResponseText(arrowBody); v.Capture {
		t.Fatalf("a response whose only glyphs are prose arrows was captured as %q — on the "+
			"measured corpus that arm would swallow 58%% of all bodies and starve the table arm",
			v.Tier)
	}

	// GREEN: box drawing is a diagram.
	if v := classifyResponseText(respGateBodyOf(t, respCaptureMinRunes+1, respGateTestBoxDiagram)); !v.Capture || v.Tier != respCaptureTierTopology {
		t.Fatalf("a box diagram was captured=%v tier=%q", v.Capture, v.Tier)
	}
}

// TestResponseTopologyGlyphsPartitionTheNoteSet keeps the two glyph sets ONE
// vocabulary (Rule 14): the response arm's box-drawing set plus its arrow set
// must be exactly the note gate's set, or a glyph has been added to one side and
// stranded on the other — which is how a "response version" quietly becomes a
// second, drifting definition.
func TestResponseTopologyGlyphsPartitionTheNoteSet(t *testing.T) {
	note := map[rune]bool{}
	for _, r := range selfNoteTopologyGlyphs {
		note[r] = true
	}
	response := map[rune]bool{}
	for _, r := range respCaptureTopologyGlyphs + respCaptureArrowGlyphs {
		response[r] = true
	}
	var missing, extra []string
	for r := range note {
		if !response[r] {
			missing = append(missing, string(r))
		}
	}
	for r := range response {
		if !note[r] {
			extra = append(extra, string(r))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("the response gate's glyph sets no longer partition selfNoteTopologyGlyphs:\n"+
			"  in the note set, absent from the response sets: %q\n"+
			"  in the response sets, absent from the note set: %q",
			missing, extra)
	}
	// And the arms the sets drive must stay disjoint: the box set must NOT
	// contain an arrow (else the topology arm would fire on prose again).
	for _, r := range respCaptureArrowGlyphs {
		if strings.ContainsRune(respCaptureTopologyGlyphs, r) {
			t.Fatalf("glyph %q is in BOTH response sets — the arms are not a partition", string(r))
		}
	}
}

// --- the regression the epic was filed for ------------------------------------

// TestVolcengineBillResponseIsCapturedAsTable is the regression case named in the
// epic's acceptance clause: the operator's own billing round — a four-column
// markdown price table in the response body, read off /home/nick/work/audio-bill.png
// and written out by the model — must be captured by the TABLE arm.
//
// THE FIXTURE IS THE REAL ROUND, byte for byte, read from a local session log and
// committed as testdata (internal/agent/testdata/volcengine-bill-response.txt):
// a hand-typed stand-in would let the gate and the fixture agree on a shape
// nobody ever wrote. The same file carries the round's [IMPORTANT] note, which is
// what makes the RED half possible without a second fixture — the note and the
// body are the SAME BYTES' two halves.
func TestVolcengineBillResponseIsCapturedAsTable(t *testing.T) {
	path := filepath.Join("testdata", "volcengine-bill-response.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the recorded round (%s): %v", path, err)
	}
	body := strings.ReplaceAll(string(raw), "\r\n", "\n")

	// The fixture must still BE the round this test claims: a table, a note, and
	// no drift that would make the assertions below vacuous.
	if !strings.Contains(body, "[IMPORTANT]") {
		t.Fatalf("%s no longer carries the round's note — the red half of this test "+
			"would prove nothing", path)
	}
	if n := strings.Count(body, "\n|"); n < 4 {
		t.Fatalf("%s carries only %d table row(s) — the recorded round's table has more",
			path, n)
	}

	// GREEN: the table is captured.
	v := classifyResponseText(body)
	if !v.Capture || v.Tier != respCaptureTierTable {
		t.Fatalf("the operator's billing round was captured=%v tier=%q — the table arm must "+
			"take it (this is the defect the epic was filed for)", v.Capture, v.Tier)
	}

	// RED: the same round's NOTE is what the gate used to see, and the note is
	// not a table — it is `；`-joined prose with ONE pipe. That is why the table
	// died with the round.
	note := selfNoteFromBody(body)
	if note == "" {
		t.Fatalf("%s carries no [IMPORTANT] block to take the red half from", path)
	}
	if nv := classifySelfNote(note); nv.Capture {
		t.Fatalf("the NOTE gate captured this round's note as %q — the premise this task was "+
			"filed on has changed; re-derive the epic before trusting either verdict", nv.Tier)
	}
}

// selfNoteFromBody returns the text of the FIRST [IMPORTANT] block in a round, or
// "" when there is none. It is the test's own reading of the contract (the
// production extractor, recordSelfNotes, is the authority for what a note IS —
// this helper only has to find the block the fixture carries).
func selfNoteFromBody(body string) string {
	open := strings.Index(body, selfNoteMarkerOpen)
	if open < 0 {
		return ""
	}
	rest := body[open+len(selfNoteMarkerOpen):]
	closeAt := strings.Index(rest, selfNoteMarkerClose)
	if closeAt < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:closeAt])
}

// --- the two gates stay disjoint ---------------------------------------------

// TestVisibleResponseBodyStripsTheNoteAndOnlyTheNote pins the stripping rule: the
// note block is protocol, and the body that remains is what the reader saw. An
// UNCLOSED marker is prose ABOUT the contract (this session's own answers are
// full of them) and must survive.
func TestVisibleResponseBodyStripsTheNoteAndOnlyTheNote(t *testing.T) {
	t.Run("a note in the middle is removed, both sides kept", func(t *testing.T) {
		got := visibleResponseBody("前文。\n" + selfNoteMarkerOpen + "\n本轮：做了事。\n" + selfNoteMarkerClose + "\n后文。")
		if !strings.Contains(got, "前文。") || !strings.Contains(got, "后文。") {
			t.Fatalf("the prose around the note was dropped: %q", got)
		}
		if strings.Contains(got, "本轮：做了事。") {
			t.Fatalf("the note survived the strip: %q", got)
		}
	})
	t.Run("an unclosed marker is prose, not a block", func(t *testing.T) {
		text := "我们的自记录协议要求以 " + selfNoteMarkerOpen + " 开头。"
		if got := visibleResponseBody(text); got != text {
			t.Fatalf("an unclosed delimiter was treated as a block:\n got %q\nwant %q", got, text)
		}
	})
	t.Run("no marker at all is returned trimmed", func(t *testing.T) {
		if got := visibleResponseBody("  正文  "); got != "正文" {
			t.Fatalf("got %q", got)
		}
	})
}

// TestResponseGateDoesNotSeeTheNoteThroughTheBody is the property that keeps ONE
// capture per round: a body whose ONLY content is a note is refused as
// note-only, so the note path keeps sole ownership of that text.
func TestResponseGateDoesNotSeeTheNoteThroughTheBody(t *testing.T) {
	note := selfNoteMarkerOpen + "\n" + strings.Repeat("结论：实测如此。", 100) + "\n" + selfNoteMarkerClose
	v := classifyResponseText(note)
	if v.Capture || v.Reason != respCaptureNoteOnly {
		t.Fatalf("a note-only round was captured=%v reason=%q — the note gate would store the "+
			"same text a second time", v.Capture, v.Reason)
	}
	// The note TEXT alone (what the note gate classifies) is a capture there —
	// proving the two gates really do see different objects.
	if nv := classifySelfNote(selfNoteFromBody(note)); !nv.Capture {
		t.Fatalf("the note gate refused its own text (%q) — the fixtures disagree about what a "+
			"conclusion looks like", nv.Reason)
	}
}

// TestResponseGateIsAPureFunctionOfItsText is the epic's hard constraint made
// checkable: no clock, no state, no network, no environment — the same bytes give
// the same verdict, twice, and the classifier reads nothing outside its argument.
func TestResponseGateIsAPureFunctionOfItsText(t *testing.T) {
	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)
	first := classifyResponseText(body)
	for i := 0; i < 3; i++ {
		if got := classifyResponseText(body); got != first {
			t.Fatalf("call %d gave %+v, first call gave %+v — the gate is not a pure function",
				i+2, got, first)
		}
	}
	// And it does not MUTATE its input (a classifier that rewrote the body in
	// place would make the caller's dedup key — the body — unreliable).
	before := body
	_ = classifyResponseText(body)
	if body != before {
		t.Fatal("classifyResponseText modified its argument")
	}
}

// TestExportClassifyResponseTextMatchesTheGate keeps the CLI's read-only face
// honest (Rule 14): the survey verb classifies through agent.ClassifyResponseText,
// so if the exported wrapper ever grew its own opinion, the hit-rate a report
// quotes would be about a different gate than the one that stores.
func TestExportClassifyResponseTextMatchesTheGate(t *testing.T) {
	for _, body := range []string{
		respGateBodyOf(t, respCaptureMinRunes+1, respGateTestBoxDiagram),
		respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow),
		respGateBodyOf(t, respCaptureConclusionRunes+1, "结论：实测如此。"),
		respGateBodyOf(t, respCaptureMinRunes+1, ""),
		"短。",
		selfNoteMarkerOpen + "x" + selfNoteMarkerClose,
	} {
		v := classifyResponseText(body)
		tier, reason := ClassifyResponseText(body)
		if tier != v.Tier || reason != v.Reason {
			t.Fatalf("ClassifyResponseText(body)=(%q,%q) but classifyResponseText gave (%q,%q)",
				tier, reason, v.Tier, v.Reason)
		}
		if tier == "" && reason == "" {
			t.Fatal("a refused body must name its reason — an empty verdict is unanswerable in a log")
		}
		if tier != "" && reason != "" {
			t.Fatalf("a captured body carried a refusal reason too (%q/%q)", tier, reason)
		}
	}
}

// --- the store, end to end ---------------------------------------------------

// TestCaptureResponseTextStoresARowWithItsPointer is the end-to-end half: a
// shaped RESPONSE becomes a distilled_qa row, and the row carries the session and
// the round of the run that produced it — the same pointer contract the note path
// owes, reached from the other object.
//
// The row's `question`/`answer` ARE the visible body (the note block stripped), so
// what a later `memory search` returns is the text the operator read, not the
// protocol inside it.
func TestCaptureResponseTextStoresARowWithItsPointer(t *testing.T) {
	ag := newCaptureTestAgent(t, 5)
	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)

	tier := ag.captureResponseText(body)
	if tier != respCaptureTierTable {
		t.Fatalf("captureResponseText returned %q, want %q", tier, respCaptureTierTable)
	}
	id, session, round, source := capturedRow(t, body)
	if id <= 0 {
		t.Fatal("the capture reported a tier but no row is readable")
	}
	if session != "capture-probe-session" || round != 5 {
		t.Errorf("row pointer = (%s,%d), want the run's own (capture-probe-session,5)", session, round)
	}
	if source != respCaptureMemorySource {
		t.Errorf("sources = %q, want %q — a reader must be able to tell a response capture "+
			"from the model's own note", source, respCaptureMemorySource)
	}
}

// TestCaptureResponseTextStripsTheNoteFromTheStoredRow: the stored row is the
// VISIBLE body. A row that carried the [IMPORTANT] block would make search return
// the harness' protocol text as if it were the answer — and would store the note
// twice, which the note-only refusal exists to prevent.
func TestCaptureResponseTextStripsTheNoteFromTheStoredRow(t *testing.T) {
	ag := newCaptureTestAgent(t, 6)
	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow) +
		"\n" + selfNoteMarkerOpen + "\n本轮：读了账单。\n" + selfNoteMarkerClose
	want := visibleResponseBody(body)

	if tier := ag.captureResponseText(body); tier != respCaptureTierTable {
		t.Fatalf("captureResponseText returned %q, want %q", tier, respCaptureTierTable)
	}
	if strings.Contains(want, selfNoteMarkerOpen) {
		t.Fatal("the fixture's visible body still contains the note marker")
	}
	// capturedRow FAILS the test when the row is absent, so this line IS the
	// assertion: the stored question is the stripped body, not the round's text.
	_, _, _, _ = capturedRow(t, want)
}

// TestCaptureResponseTextRefusesASessionlessRun mirrors the note path: with no
// session there is no pointer to stamp, and NO anonymous row is written.
func TestCaptureResponseTextRefusesASessionlessRun(t *testing.T) {
	testutil.NewTestDBPath(t)
	ag := &Agent{hippocampusRound: 7, Logger: log.New(io.Discard, "", 0)}
	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)

	if tier := ag.captureResponseText(body); tier != "" {
		t.Fatalf("a session-less run captured %q — the row would carry no pointer", tier)
	}
	if rows := responseRowsWithSource(t, body, "question"); rows != 0 {
		t.Fatalf("a session-less run wrote %d anonymous row(s)", rows)
	}
}

// TestCaptureResponseTextIsLocalAndIdempotent: the same body twice in one run pays
// one store. The dedup key is namespaced, so a note and a body carrying the same
// text cannot suppress each other (respStoredKeyPrefix).
func TestCaptureResponseTextIsLocalAndIdempotent(t *testing.T) {
	ag := newCaptureTestAgent(t, 8)
	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)

	if tier := ag.captureResponseText(body); tier == "" {
		t.Fatal("the first capture stored nothing — the dedup assertion would be vacuous")
	}
	if tier := ag.captureResponseText(body); tier != "" {
		t.Fatalf("the second capture returned %q — the run-local dedup did not fire", tier)
	}
}

// TestCaptureResponseTextNeverSatisfiesTheMemoryRequirement is the epic's
// constraint carried to the second object: the harness' own capture must not set
// a.memoryStored, or the task-end checkpoint that makes the MODEL store a lesson
// would be silently retired.
func TestCaptureResponseTextNeverSatisfiesTheMemoryRequirement(t *testing.T) {
	ag := newCaptureTestAgent(t, 9)
	if ag.memoryStored {
		t.Fatal("fixture starts with memoryStored set")
	}
	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)
	if ag.captureResponseText(body) == "" {
		t.Fatal("the test must exercise a REAL capture")
	}
	if ag.memoryStored {
		t.Fatal("captureResponseText set memoryStored — the task-end memory requirement would never fire again")
	}
}

// TestCaptureResponseTextRefusesWhatTheGateRefuses closes the seam between the
// predicate and the store: a refused body writes nothing at all. Without this,
// classifyResponseText and captureResponseText could disagree and the store would
// silently become the gate.
func TestCaptureResponseTextRefusesWhatTheGateRefuses(t *testing.T) {
	ag := newCaptureTestAgent(t, 10)
	for _, body := range []string{
		"收到。",
		respGateBodyOf(t, respCaptureMinRunes+1, ""),
		selfNoteMarkerOpen + respGateFiller(respCaptureConclusionRunes+10) + selfNoteMarkerClose,
	} {
		if tier := ag.captureResponseText(body); tier != "" {
			t.Fatalf("a refused body was captured as %q", tier)
		}
	}
	if rows := responseRowsWithSource(t, respCaptureMemorySource, "sources"); rows != 0 {
		t.Fatalf("refused bodies wrote %d row(s)", rows)
	}
}

// responseRowsWithSource counts distilled_qa rows matching a column, treating a
// LEDGER THAT WAS NEVER OPENED as zero rows — the same shape the note path's
// refusal test uses (selfnotecapture_test.go): a store that never ran never
// created the table, which is also "no row was written".
func responseRowsWithSource(t *testing.T, value, column string) int {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(testDBPathFromEnv(t)))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var present int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='distilled_qa'").Scan(&present); err != nil {
		t.Fatalf("probe schema: %v", err)
	}
	if present == 0 {
		return 0
	}
	var rows int
	// #nosec G201 — `column` is a caller-supplied constant in this file, never
	// input; the value is bound.
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa WHERE "+column+" = ?", value).Scan(&rows); err != nil {
		t.Fatalf("probe rows: %v", err)
	}
	return rows
}

// --- the round-boundary wiring (Rule 31) -------------------------------------

// TestRunCapturesTheResponseBodyAtTheRoundBoundary drives a REAL agent round
// through the loop's own capture call site, against a mock endpoint that answers
// with the operator's billing table.
//
// WHY THIS AND NOT ONLY THE UNIT TESTS ABOVE: those prove the gate and the store;
// they cannot prove the CALL is where it must be — a capture wired after the
// finish_reason switch, or inside a branch a text-only round never takes, would
// pass every test in this file and store nothing in production. This test enters
// through the same entry point a real run does (Agent.Run), with the same session
// logger and the same ledger the capture needs, so a row appearing in the
// database IS the wiring working.
func TestRunCapturesTheResponseBodyAtTheRoundBoundary(t *testing.T) {
	testutil.NewTestDBPath(t) // pins GOPI_MEMORY_DB at a fresh per-test ledger (Rule 32)
	sl, err := NewSessionLoggerForRun("capture-round-probe", filepath.Join(t.TempDir(), "raw.log"),
		"capture-round-session", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	t.Cleanup(func() { _ = sl.Close() }) //stylegate:ignore cleanup close of the response-capture fixture's logger, after its records were read

	body := respGateBodyOf(t, respCaptureMinRunes+1, respGateTestTableRow)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, body, "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		SessionLog:    sl,
		Logger:        log.New(io.Discard, "", 0),
	}
	if _, err := ag.Run("把账单口径对齐一下"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// The row is the assertion: the body the round produced is searchable.
	id, session, round, source := capturedRow(t, body)
	if id <= 0 {
		t.Fatal("the round ran but no distilled_qa row carries its body — the capture is not wired at the round boundary")
	}
	if session != "capture-round-session" {
		t.Errorf("session_id = %q, want the run's own session", session)
	}
	// The pointer must name a REAL round. The exact number is not pinned: the
	// loop re-issues a round whose first end_turn it does not trust, and both
	// rounds carry the same body — the run-local dedup keeps the FIRST row, so
	// the recorded round is the one that produced it, not the last one.
	if round < 1 {
		t.Errorf("round_index = %d — the row carries no round pointer", round)
	}
	if source != respCaptureMemorySource {
		t.Errorf("sources = %q, want %q", source, respCaptureMemorySource)
	}
}
