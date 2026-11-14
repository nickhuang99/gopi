package testsuite

// turnfold_budget_test.go — the turn fold measured on the WIRE, through the real
// binary (PROJECT.md Rule 31: a surface is executed, not described).
//
// The unit arms in internal/agent/turnfold_test.go prove the fold's arithmetic on
// a hand-built conversation. What they CANNOT prove is that the fold is reached
// at all — a boundary nobody calls is a correct function in an unreachable place.
// This walk therefore drives `gopi chat --once --tools` with a scripted mock that
// asks for a tool on every round, and asserts the thing the epic is about ON THE
// REQUEST BODIES the mock actually received:
//
//	turn 2's request must carry NO tool_use/tool_result blocks from turn 1 — only
//	the note that replaced them.
//
// The comparison is a NUMBER per request (how many raw tool blocks the wire
// carried), never a substring, because a substring in a body could be the system
// prompt or the note's own prose describing what happened.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// turnFoldWireToolBlocks counts, in the n-th recorded request, the messages that
// are RAW TOOL TRAFFIC: an assistant message carrying tool_use blocks, or a user
// message carrying tool_result blocks.
//
// The wire shapes are the Claude Messages ones: an assistant message with
// `content: [{type:"tool_use", …}]` and a user message with
// `content: [{type:"tool_result", …}]`. A message whose content is a bare string
// is prose by construction and is skipped.
func turnFoldWireToolBlocks(t *testing.T, llm *testutil.MockLLM, n int) (toolUse, toolResult int) {
	t.Helper()
	body := llm.RequestBody(n)
	if body == "" {
		t.Fatalf("request %d is not recorded — nothing to measure", n)
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request %d is not a Claude messages body: %v", n, err)
	}
	for _, m := range req.Messages {
		var blocks []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue // a bare string message: prose
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				toolUse++
			case "tool_result":
				toolResult++
			}
		}
	}
	return toolUse, toolResult
}

// turnFoldWireUserTexts returns the text of every user message on the n-th
// request (both a bare string and a text block), so a claim about what was SENT
// can be made about the message list rather than about the raw bytes.
func turnFoldWireUserTexts(t *testing.T, llm *testutil.MockLLM, n int) []string {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(llm.RequestBody(n)), &req); err != nil {
		t.Fatalf("request %d is not a Claude messages body: %v", n, err)
	}
	var out []string
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			out = append(out, s)
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue
		}
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, "\n"))
		}
	}
	return out
}

// turnFoldEnvKeepTurns is the env name the fold's raw window is configured by
// (internal/agent/turnfold.go's turnFoldRecentTurnsEnv). Spelled here as a
// literal because it is a SUPERVISOR-FACING name — the walks drive the real
// binary, so they must set the variable a user sets, not an internal symbol.
const turnFoldEnvKeepTurns = "GOPI_TURN_FOLD_KEEP_TURNS"

// turnFoldSmallWindowEnv is MockEnv plus the ONE declaration these walks need.
//
// THE FOLD IS NO LONGER THRESHOLD-TRIGGERED (turnfold.go: commit 4c8c44df gated
// it on the compaction trigger and that was measured inert — a 39-turn, 93k-token
// session against a 150k trigger folded nothing and still carried 135 raw
// tool_result blocks). It runs at every round boundary, so a walk does not have to
// declare a context window to make it fire; the window is declared only because
// the binary still carries one.
//
// THE RAW WINDOW IS DECLARED AS ZERO, which is also the shipped default: no turn
// is protected, so EVERY completed turn is replaced by its note. Declaring it
// explicitly rather than inheriting it is what makes the walk hermetic — MockEnv
// strips every ambient GOPI_* var, so the value the binary runs with is the value
// written here.
const turnFoldSmallWindow = 20

func turnFoldSmallWindowEnv(t *testing.T, gopiHome, mockURL string) []string {
	t.Helper()
	return append(testutil.MockEnv(t, gopiHome, mockURL),
		"GOPI_CONTEXT_WINDOW="+strconv.Itoa(turnFoldSmallWindow),
		turnFoldEnvKeepTurns+"=0")
}

// TestTurnFold_SecondTurnCarriesNoRawToolTraffic is the epic's headline walk.
//
// THE SESSION SHAPE. `gopi chat --once` with a scripted transcript opens TWO
// turns: the first line is a message, `/quit` ends the session. A tool round
// inside the first turn does not open a new turn (a tool result is not an
// input), so the fold has nothing to do until the SECOND message arrives — which
// is exactly the moment the epic describes ("a completed turn is replaced when
// the next input arrives").
//
// So the walk is: message → the mock asks for a read → the mock answers → a
// second message → the mock sees the request. That last request is the one that
// must be free of turn 1's raw tool traffic.
func TestTurnFold_SecondTurnCarriesNoRawToolTraffic(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	probe := "turnfold-probe.txt"
	if err := os.WriteFile(filepath.Join(work, probe), []byte("probe content\n"), 0o644); err != nil {
		t.Fatalf("fixture write: %v", err)
	}

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ // turn 1, round 1: a tool
			ID:   "call_fold_1",
			Name: "read",
			Args: map[string]interface{}{"path": probe},
		}},
		testutil.Resp{Text: "first turn finished, and it read " + probe + " on the way"}, // turn 1, round 2
		testutil.Resp{Text: "second turn answered, nothing else to add"},                 // turn 2
		testutil.Resp{Text: "third turn answered, wrapping up here"},                     // turn 3
	)
	defer llm.Close()

	// The transcript opens with a COMMAND, which is what tells `--once` the caller
	// is driving a SESSION rather than asking one question (chat_once_seq.go):
	// without it the two message lines would be folded into ONE message and the
	// session would have no completed turn to fold.
	// THE WINDOW IS DECLARED, because the fold is THRESHOLD-TRIGGERED (turnfold.go's
	// turnFoldDue): a chat's default window is DefaultContextWindow (200000), and a
	// two-message fixture prices nowhere near 0.75 × that, so with the default the
	// fold correctly does nothing and this walk would be measuring the threshold
	// rather than the substitute. A 200-token window puts this conversation over
	// the line while leaving the policy itself untouched — the walk drives the
	// same real binary, the same chat surface and the same fold.
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"/tools\nfirst message please\nsecond message please\nthird message please\n/quit\n",
		turnFoldSmallWindowEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	total := llm.RequestCount()
	if total < 3 {
		t.Fatalf("the mock served %d request(s); want ≥3 (turn 1 with its tool round, then turn 2) — the fold cannot be measured on a session that never had a completed turn", total)
	}

	// THE MEASUREMENT. Request 1 is turn 1's opener: it has NO tool blocks by
	// construction. The LAST request is turn 2's — and turn 1 is complete by
	// then, so its tool_use/tool_result pair must be GONE.
	firstUse, firstResult := turnFoldWireToolBlocks(t, llm, 1)
	if firstUse != 0 || firstResult != 0 {
		t.Fatalf("request 1 already carries raw tool traffic (%d tool_use, %d tool_result) — the fixture is wrong", firstUse, firstResult)
	}
	lastUse, lastResult := turnFoldWireToolBlocks(t, llm, total)
	if lastUse != 0 || lastResult != 0 {
		t.Fatalf("the last request still carries %d tool_use / %d tool_result block(s) although turn 1 was COMPLETE — the fold did not run",
			lastUse, lastResult)
	}

	// The note that replaced it IS on the wire, and it is a user row the harness
	// wrote (the fold's shape), not an assistant message.
	var noteSeen bool
	for _, text := range turnFoldWireUserTexts(t, llm, total) {
		if strings.Contains(text, "turn folded") {
			noteSeen = true
		}
	}
	if !noteSeen {
		t.Fatalf("no turn note is on the last request's user rows — turn 1's history vanished without a substitute.\nuser rows: %q",
			turnFoldWireUserTexts(t, llm, total))
	}

	// Both turns' own text survives: the fold must not swallow the conversation.
	body := llm.RequestBody(total)
	for _, want := range []string{"first message please", "second message please", "third message please"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the last request lost %q — the fold removed more than the raw traffic", want)
		}
	}
}

// turnFoldWireRecordedNote is the phrase the recorded note below carries. It is
// CJK on purpose: a note is capped in RUNES, so a byte-based budget would cut a
// Chinese sentence three times shorter than an English one — the second defect
// this walk measures.
const turnFoldWireRecordedNote = "折叠 note 必须优先拷贝 self-record 原文，四栏只作兜底"

// TestTurnFold_RecordedNoteSurvivesOnTheWire is the defect's WIRE-LEVEL proof,
// through the real binary (Rule 31).
//
// The unit arms show the note RENDERS the recorded note. What they cannot show is
// that a real session's recorded note reaches the request at all: the model's
// `[IMPORTANT]` block lives inside an assistant message that the fold REMOVES, so
// the only path by which its words can still be on the wire is the copy into the
// substitute note. The mock therefore answers turn 1 with a tool call and then
// with a record block, and the LAST request is searched for the note's own words.
func TestTurnFold_RecordedNoteSurvivesOnTheWire(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	probe := "turnfold-record-probe.txt"
	if err := os.WriteFile(filepath.Join(work, probe), []byte("probe content\n"), 0o644); err != nil {
		t.Fatalf("fixture write: %v", err)
	}

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ // turn 1, round 1: a tool
			ID:   "call_fold_rec",
			Name: "read",
			Args: map[string]interface{}{"path": probe},
		}},
		// turn 1, round 2: the model's own record block, i.e. the knowledge the
		// four mechanical slots cannot re-derive.
		testutil.Resp{
			Text: "read it and the answer is in the file\n" +
				"[IMPORTANT]\n" + turnFoldWireRecordedNote + "\n[/IMPORTANT]",
			// The fragmenter is RUNE-SAFE at a non-zero width; the legacy
			// two-half split (0) cuts on a BYTE index, and a CJK answer sliced
			// mid-rune is a wire shape no real server emits (it also dropped a
			// character of the note, which is what made this arm red for a
			// reason that had nothing to do with the fold).
			TextDeltaBytes: 16,
		},
		testutil.Resp{Text: "second turn answered"}, // turn 2
		testutil.Resp{Text: "third turn answered"},  // turn 3
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"/tools\nfirst message please\nsecond message please\nthird message please\n/quit\n",
		turnFoldSmallWindowEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	total := llm.RequestCount()
	if total < 3 {
		t.Fatalf("the mock served %d request(s); want ≥3 so that a completed turn exists", total)
	}
	body := llm.RequestBody(total)

	// THE MEASUREMENT, two-sided. The raw message that CARRIED the record block
	// must be gone (the substitute contract — the fold replaces, it does not
	// append). The check is on the assistant message's own prose, not on the
	// "[IMPORTANT]" spelling: the harness's SELF-RECORDING rule message quotes
	// the delimiters in every request, so a marker substring measures the rule,
	// not the fold.
	// The record block appears exactly ONCE on the wire — in the harness's
	// SELF-RECORDING rule message — when the fold replaced the turn. A second
	// occurrence is the raw assistant message still being carried, i.e. the
	// "addition" the substitute contract forbids.
	if got := strings.Count(body, "[/IMPORTANT]"); got != 1 {
		t.Fatalf("the last request carries %d record-block closers, want exactly 1 (the rule message) — the raw record block survived the fold", got)
	}
	// … and the knowledge it carried is STILL THERE, in the note's own
	// recorded-knowledge slot.
	if !strings.Contains(body, turnFoldWireRecordedNote) {
		t.Fatalf("the last request lost the recorded note %q — the fold discarded the turn's own knowledge\nnote row: %q", turnFoldWireRecordedNote, uiNoteRow(t, llm, total))
	}
	var noteRow string
	for _, text := range turnFoldWireUserTexts(t, llm, total) {
		if strings.Contains(text, "turn folded") {
			noteRow = text
		}
	}
	if noteRow == "" {
		t.Fatalf("no turn note on the last request's user rows: %q", turnFoldWireUserTexts(t, llm, total))
	}
	if !strings.Contains(noteRow, "recorded: ") {
		t.Fatalf("the note on the wire carries no recorded-knowledge slot:\n%s", noteRow)
	}
	t.Logf("the fold note on request %d:\n%s", total, noteRow)
}

func uiNoteRow(t *testing.T, llm *testutil.MockLLM, n int) string {
	t.Helper()
	for _, text := range turnFoldWireUserTexts(t, llm, n) {
		if strings.Contains(text, "turn folded") {
			return text
		}
	}
	return ""
}

// TestTurnFold_NoteOnTheWireIsAVerbatimCopy is the operator's four complaints
// measured on the REQUEST BODY the real binary sent (Rule 31).
//
// The unit arms in internal/agent prove the renderer's shape on a hand-built
// conversation; this walk proves the shape survives the whole path — chat input
// line → message → record file → fold → wire — because every one of those hops
// is somewhere a carrier can be re-introduced or a body re-flowed.
//
// THE FOUR ASSERTIONS ARE THE FOUR COMPLAINTS (2026-11-10):
//  1. no scaffolding head sentence;
//  2. no dictation carrier in the ask line;
//  3. no "not kept here" notice for an input that fits;
//  4. no mechanical counts line, because the record file answered.
//
// Plus the positive half: the recorded note is on the wire byte for byte, and
// the operator's own dictated words are in the ask.
func TestTurnFold_NoteOnTheWireIsAVerbatimCopy(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	probe := "turnfold-verbatim-probe.txt"
	if err := os.WriteFile(filepath.Join(work, probe), []byte("probe content\n"), 0o644); err != nil {
		t.Fatalf("fixture write: %v", err)
	}

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_verbatim",
			Name: "read",
			Args: map[string]interface{}{"path": probe},
		}},
		testutil.Resp{
			Text:           "read it\n[IMPORTANT]\n" + turnFoldWireVerbatimNote + "\n[/IMPORTANT]",
			TextDeltaBytes: 16,
		},
		testutil.Resp{Text: "second turn answered"},
		testutil.Resp{Text: "third turn answered"},
	)
	defer llm.Close()

	// THE FIRST MESSAGE IS SHAPED LIKE A DICTATED ONE: the chat producer prepends
	// sessionevent.VoiceInputPrefix (+ its separator) to the operator's text, so
	// the message the fold reads is carrier + words.
	//
	// IT IS ONE STDIN LINE, and that is a fixture constraint rather than a
	// simplification of the defect: the chat CLI reads ONE MESSAGE PER LINE, so a
	// carrier with its double-newline separator would arrive as TWO messages and
	// the ask line would measure the carrier alone. The carrier TEXT is exactly
	// the one production prepends; only the separator is a space here.
	dictated := "我们自己就是代码，我们自己的代码就应该成为这个 prompt 的一部分，" +
		"help 一改这个 prompt 就会过时，所以这些命令索引必须是源代码生成的。"
	first := sessionevent.VoiceInputPrefix + " " + dictated

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"/tools\n"+first+"\nsecond message please\nthird message please\n/quit\n",
		turnFoldSmallWindowEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	total := llm.RequestCount()
	if total < 3 {
		t.Fatalf("the mock served %d request(s); want ≥3 so that a completed turn exists", total)
	}
	var noteRow string
	for _, text := range turnFoldWireUserTexts(t, llm, total) {
		if strings.Contains(text, "turn folded") {
			noteRow = text
		}
	}
	if noteRow == "" {
		t.Fatalf("no turn note on the last request's user rows: %q", turnFoldWireUserTexts(t, llm, total))
	}
	// THE EXPECTED BODY IS THE RECORD FILE'S OWN LINE, read off disk — not the
	// constant above. The WRITER owns normalisation (collapseSelfNote,
	// selfrecord.go: a block is flattened to one line before it is stored), so
	// "verbatim" is a claim about the fold's copy of the FILE, and a test that
	// compared against the model's raw text would be measuring the writer.
	recorded := turnFoldRecordFileNote(t, home)
	t.Logf("the fold note on request %d:\n%s", total, noteRow)

	// 1. THE HEAD IS MINIMAL.
	if strings.Contains(noteRow, "the history this note replaces") {
		t.Fatalf("the note's head still carries the scaffolding sentence:\n%s", noteRow)
	}
	// 2. NO DICTATION CARRIER — neither the provenance sentence nor its marker.
	if strings.Contains(noteRow, sessionevent.VoiceInputPrefix) {
		t.Fatalf("the note still carries the dictation carrier:\n%s", noteRow)
	}
	// …and the operator's words ARE there, so the carrier was STRIPPED and not
	// the whole ask thrown away.
	if !strings.Contains(noteRow, "我们自己的代码就应该成为这个 prompt") {
		t.Fatalf("the note lost the operator's own words instead of just the carrier:\n%s", noteRow)
	}
	// 3. NO TRUNCATION NOTICE: the dictate above fits the ask budget, so a "not
	// kept here" line here is the defect this epic is about (it fired on nearly
	// every CJK turn).
	if strings.Contains(noteRow, "not kept here") {
		t.Fatalf("the truncation notice fired on an input that fits:\n%s", noteRow)
	}
	// 4. NO MECHANICAL COUNTS: the record file answered this turn.
	if strings.Contains(noteRow, "what this turn did") {
		t.Fatalf("the note still carries the mechanical counts line:\n%s", noteRow)
	}
	// AND THE POSITIVE HALF: the recorded note is a VERBATIM copy.
	if recorded == "" {
		t.Fatalf("this run wrote no self-record.md note, so the copy path never had an input")
	}
	if !strings.Contains(noteRow, turnFoldNoteBulletOf(recorded)) {
		t.Fatalf("the note on the wire is not a verbatim copy of the record file's line %q:\n%s", recorded, noteRow)
	}
	// AND NO RENDERER MARK OF ITS OWN: the copy used to be `- <note>`, a dash the
	// record file never writes, so "verbatim" was true only of a suffix.
	if strings.Contains(noteRow, "- "+recorded) {
		t.Fatalf("the recorded slot prepends a renderer mark the file does not have:\n%s", noteRow)
	}
}

// turnFoldWireVerbatimNote is the note the mock records. It carries an INTERIOR
// double space on purpose: a copy that ran the body through a whitespace
// normaliser would still be a substring of itself, so the fixture makes the
// normaliser's output differ from the source.
const turnFoldWireVerbatimNote = "折叠 note 必须是 self-record 原文的逐字拷贝，  中间的双空格也要原样保留，不能重排"

// turnFoldRecordFileNote returns the LAST note line the run's own
// `<session>/self-record.md` holds — read from the run's home, through the same
// bullet the fold renders, so the comparison is against the FILE and not against
// what the model typed.
func turnFoldRecordFileNote(t *testing.T, home string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".gopi", "sessions", "*", "self-record.md"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no self-record.md under %s (err=%v)", home, err)
	}
	body, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		t.Fatalf("read %s: %v", matches[len(matches)-1], err)
	}
	lines := strings.Split(string(body), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		// THE STAMP IS THE WRITER'S, and it is `t<turn> ` (internal/agent's
		// selfRecordTurnStampFmt; `t? ` when the turn is unknown). It used to be
		// `r<round> `, so a reader keyed on "r" stopped matching the file the
		// moment the axis was renamed — and the arm then reported "this run wrote
		// no self-record.md note" about a run that had written several.
		if rest, ok := strings.CutPrefix(line, "t"); ok {
			if sp := strings.IndexByte(rest, ' '); sp > 0 {
				return rest[sp+1:]
			}
		}
		if rest, ok := strings.CutPrefix(line, "- "); ok && strings.TrimSpace(rest) != "" {
			return rest
		}
	}
	return ""
}

// turnFoldNoteBulletOf is the line the fold renders for one copied note: the
// file's own text and a newline, with NO renderer mark — the `recorded:` slot
// copies verbatim (measured 2026-11-14: it used to prepend "- ", which
// self-record.md never writes). Spelled here rather than reaching into
// internal/agent (a test package cannot import an unexported const).
func turnFoldNoteBulletOf(note string) string { return note + "\n" }
