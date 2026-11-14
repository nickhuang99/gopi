package gopilog

// chat_roundorder_test.go — THE ROW ORDER OF THE CHAT WRITER'S SESSION JSONL,
// pinned as the round numbering depends on it.
//
// WHY THIS LIVES IN internal/gopilog. The numbering under test is gopilog's
// own (`SessionRounds`, `SessionCanonicalRounds` — what `gopi session log <id>
// --rounds` prints and what `--round N` selects), and the only way to test it
// against a REAL chat run is to drive the writer and read the file back through
// this package's reader. internal/agent cannot host that fixture: gopilog
// imports agent (formatter.go), so an agent-package test importing gopilog is an
// import cycle. gopilog already imports agent for the task-list parsing, so the
// fixture belongs here.
//
// THE DEFECT (measured on session d8943792, 2026-10-11). A tool-enabled chat
// turn logs its rounds in this order (internal/agent/chat_ops.go:677/689/812):
//
//	per round  : LogAssistantToolUse(...) ; LogRequest(round, ...)
//	turn final : LogAssistantText(finalContent)
//
// The request row is a WIRE-START marker: its usage belongs to the exchange it
// STARTS, and the reader closes it with the content that precedes it. The turn's
// ANSWER is written after the LAST round's request row, so it closes the NEXT
// row — of the next turn:
//
//	request(row k)     iter=0  usage=round k
//	assistant tool_use (round k)
//	request(row k+1)   iter=1  usage=round k+1
//	assistant tool_use (round k+1)
//	request(row k+2)   iter=2  usage=round k+2
//	assistant text     ← the TURN'S ANSWER
//	user row           ← the next turn opens here
//
// Row k+2 then shows "carried no content" while its content is served under the
// FOLLOWING ordinal, and the next turn's prompt is attributed to the row the
// answer had already closed. On the measured session that is 20 of 70 rows —
// every turn's last round, 19 of them non-final turns, every one carrying
// completion_tokens > 0.
//
// THE FIX pinned here (write side, ONE place): the answer is written INSIDE the
// round that produced it — chat_ops.go logs it where the round's own text is
// accepted, so the row it follows is the row it closes.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

const (
	// chatRoundOrderAnswer is the turn's final answer text — the one string the
	// row-order defect moves between two rounds.
	chatRoundOrderAnswer = "ROUND-ORDER-ANSWER"
	// chatRoundOrderSecret is the body of the file the tool round reads, so the
	// fixture proves the read really ran.
	chatRoundOrderSecret = "ROUND-ORDER-SECRET-7788"
	// chatRoundOrderTurns is how many turns the red case runs: every turn adds
	// its own empty row (or none, once fixed), so the signature is unmistakable.
	chatRoundOrderTurns = 3
	// chatRoundOrderCallsPerTurn is how many model calls one turn of this
	// fixture makes — the tool round plus the answer round. It only decides
	// which stream the fake model returns.
	chatRoundOrderCallsPerTurn = 2
)

// ---------------------------------------------------------------------------
// FIXTURES — a REAL tool-enabled chat turn, on a real (isolated) session log.
// ---------------------------------------------------------------------------

// chatRoundOrderProfile pins the profile the fixture writes into. It must never
// be the operator's live one (PROJECT.md Rule 32): the sessions root is asserted to
// live under the temp home BEFORE any writer runs.
func chatRoundOrderProfile(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "no-settings.json"))
	t.Setenv("GOPI_RAW_LOG", "OFF")
	root, err := config.SessionsRoot()
	if err != nil {
		t.Fatalf("config.SessionsRoot: %v", err)
	}
	if rel, relErr := filepath.Rel(home, root); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("sessions root %q is outside the isolated home %q — refusing to run against the operator's profile", root, home)
	}
}

// chatRoundOrderModel is the fake model: the FIRST call of each turn returns a
// tool_use (the tool round, a real read of a real file), every later call
// returns the turn's final answer with end_turn.
func chatRoundOrderModel(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte(chatRoundOrderSecret), 0o644); err != nil {
		t.Fatalf("stage secret: %v", err)
	}
	var calls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if atomic.AddInt32(&calls, 1)%chatRoundOrderCallsPerTurn == 1 {
			agent.WriteClaudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": secret})
			return
		}
		agent.WriteClaudeTextStream(w, chatRoundOrderAnswer, "end_turn")
	}))
}

// newChatRoundOrderRun opens a session logger and a chat-ops agent that writes
// into it — the real writer, the real reader.
//
// The LOGGER is returned beside the path because the writer BUFFERS per round:
// a test that reads the file between turns must flush first, exactly as the
// surface does when a turn ends, or it reads a lagging file (measured: a second
// turn that ran fine appeared to have written nothing).
func newChatRoundOrderRun(t *testing.T, server *httptest.Server) (*agent.Agent, *agent.SessionLogger, string) {
	t.Helper()
	sl, err := agent.NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("new session logger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore fixture teardown; the handle is released at test end either way
	ag := agent.NewChatOpsTestAgent(t, server.URL, sl)
	// Multi-turn mode: one session log across turns — the shape the defect was
	// measured on. A single-turn chat closes its log at the turn boundary, so a
	// second turn could never reach the misnumbering under test.
	ag.PersistChatSession = true
	return ag, sl, sl.Path()
}

// chatRoundOrderTurn runs one chat turn and FLUSHES the log, so the state the
// turn left on disk is the state a reader sees.
func chatRoundOrderTurn(t *testing.T, ag *agent.Agent, sl *agent.SessionLogger, turn int) {
	t.Helper()
	if _, err := ag.ChatOps("turn", nil); err != nil {
		t.Fatalf("ChatOps turn %d: %v", turn, err)
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("sync session log after turn %d: %v", turn, err)
	}
}

// assertRoundOrderSound is THE ASSERTION the defect violates: every round of a
// chat session that produced content carries its content, and the answer belongs
// to the round whose `request` row it followed.
//
// A red instrument must not pass on a wrongly-shaped log for the wrong reason,
// so the fixture FIRST proves the turn produced its answer (it is somewhere in
// the log); only then does it demand the answer sit in a round of its own.
func assertRoundOrderSound(t *testing.T, path string, turn int) *Session {
	t.Helper()
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse session log %s: %v", path, err)
	}
	answerRound := -1
	for _, it := range s.Iterations {
		for _, txt := range it.TextBlocks {
			if txt == chatRoundOrderAnswer {
				answerRound = it.RequestOrdinal
			}
		}
	}
	if answerRound < 0 {
		t.Fatalf("turn %d: the answer %q is in no round of %s — the writer dropped it, so this log is not "+
			"a valid sample of the shape under test", turn, chatRoundOrderAnswer, path)
	}
	var empty []int
	for _, cr := range SessionCanonicalRounds(s) {
		if !cr.HasContent() {
			empty = append(empty, cr.Ordinal)
		}
	}
	if len(empty) > 0 {
		t.Fatalf("turn %d: round(s) %v of %d carry NO content while the turn DID produce an answer "+
			"(stamped round %d) — the answer was written OUTSIDE the round that produced it, so it closed a "+
			"later `request` row and left that row empty. `gopi session log <id> --rounds` prints exactly "+
			"this as \"(this round carried no content)\", and the following round's body is served under "+
			"the NEXT ordinal. log=%s", turn, empty, s.RequestEvents, answerRound, path)
	}
	if len(s.Iterations) != s.RequestEvents {
		t.Fatalf("turn %d: content rounds=%d against %d `request` rows — a chat turn's rounds must be "+
			"one-to-one with the rows it wrote (log=%s)", turn, len(s.Iterations), s.RequestEvents, path)
	}
	// ROUND NUMBERS CONTINUOUS 1..N — the operator-visible acceptance. A scope
	// that skips a value is the same defect seen from the other end: the skipped
	// row is the one an answer closed, and the content after it moved up.
	for i, it := range s.Iterations {
		if it.RequestOrdinal != i+1 {
			t.Fatalf("turn %d: content round #%d carries ordinals %s — a contiguous log is 1..%d; "+
				"a gap or a 0 means an answer was written outside the round that produced it (log=%s)",
				turn, i, ordinalsOf(s), s.RequestEvents, path)
		}
	}
	return s
}

// ordinalsOf renders every content round's ordinal for a failure message.
func ordinalsOf(s *Session) string {
	out := make([]string, 0, len(s.Iterations))
	for _, it := range s.Iterations {
		out = append(out, strconv.Itoa(it.RequestOrdinal))
	}
	return strings.Join(out, ",")
}

// ---------------------------------------------------------------------------
// 1. THE DEFECT'S SIGNATURE — red before the fix, green after.
// ---------------------------------------------------------------------------

// TestChatRoundOrder_NoEmptyRound runs several tool-enabled turns on ONE session
// log and demands that no round of it be contentless.
//
// RED on the unfixed writer: every turn leaves exactly one empty row — the row
// its answer closed. GREEN once the answer is written inside its own round.
func TestChatRoundOrder_NoEmptyRound(t *testing.T) {
	chatRoundOrderProfile(t)
	server := chatRoundOrderModel(t)
	defer server.Close()
	ag, sl, path := newChatRoundOrderRun(t, server)

	for turn := 1; turn <= chatRoundOrderTurns; turn++ {
		chatRoundOrderTurn(t, ag, sl, turn)
		assertRoundOrderSound(t, path, turn)
	}
}

// ---------------------------------------------------------------------------
// 2. THE ROW ORDER — the invariant the numbering rests on, in the bytes.
// ---------------------------------------------------------------------------

// TestChatRoundOrder_AnswerClosesItsOwnRow pins the JSONL order of ONE
// tool-enabled turn: an `assistant` row (the tool_use), the round's `request`
// row, then a SECOND `assistant` row — the turn's answer — all BEFORE the next
// `request` row. That order is the whole invariant: it is what lets the answer
// close the row it followed instead of the following one.
//
// This is the structural half of the red proof: it passes before and after the
// fix, so a fix that moved the answer somewhere else would fail HERE even if the
// empty-row test were somehow satisfied.
func TestChatRoundOrder_AnswerClosesItsOwnRow(t *testing.T) {
	chatRoundOrderProfile(t)
	server := chatRoundOrderModel(t)
	defer server.Close()
	ag, sl, path := newChatRoundOrderRun(t, server)

	chatRoundOrderTurn(t, ag, sl, 1)
	chatRoundOrderTurn(t, ag, sl, 2)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log %s: %v", path, err)
	}
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad JSONL line %.80q: %v", line, err)
		}
		kinds = append(kinds, ev.Type)
	}
	var reqs []int
	for i, k := range kinds {
		if k == "request" {
			reqs = append(reqs, i)
		}
	}
	if len(reqs) < 2 {
		t.Fatalf("two chat turns logged %d `request` row(s) (%v): each round must write its own row", len(reqs), kinds)
	}
	first, second := reqs[0], reqs[1]
	if first == 0 || kinds[first-1] != "assistant" {
		t.Fatalf("the row before the first `request` is %v, want the tool round's `assistant` row: %v",
			kinds[:first], kinds)
	}
	// The turn's answer must be an assistant row inside this window: after the
	// last round's request row, before the next turn's. That is the only
	// position from which it closes its OWN row.
	var lastAssistant = -1
	for i := first + 1; i < second; i++ {
		if kinds[i] == "assistant" {
			lastAssistant = i
		}
	}
	if lastAssistant < 0 {
		t.Fatalf("no `assistant` row between the first two `request` rows (%v): the turn's answer was not "+
			"written before the next turn's request row, so it cannot close its own round", kinds)
	}
	if second-lastAssistant > 2 {
		t.Fatalf("the turn's answer row (%d) is separated from the next `request` row (%d) by %d rows "+
			"(%v) — the answer must be the round's OWN last content, immediately before the row that "+
			"closes it", lastAssistant, second, second-lastAssistant-1, kinds)
	}

	// ONE ANSWER RECORD PER TURN — the contract the old single call site
	// enforced by being the only one, now enforced by WHERE it is written. Two
	// turns must leave exactly two answer records, not four: a tool round's text
	// is working notes, and re-logging a turn's answer is the `pty-okpty-ok`
	// duplicate (devdoc/chat-duplicate-assistant-record.md).
	s := assertRoundOrderSound(t, path, 2)
	answers := 0
	for _, it := range s.Iterations {
		for _, txt := range it.TextBlocks {
			if txt == chatRoundOrderAnswer {
				answers++
			}
		}
	}
	if answers != 2 {
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var ev struct {
				Type    string `json:"type"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			if ev.Type == "assistant" {
				lines = append(lines, ev.Type+" "+string(ev.Message.Content))
			} else {
				lines = append(lines, ev.Type)
			}
		}
		t.Fatalf("two chat turns left %d %q record(s), want exactly 2 (one per turn) — a turn's answer is "+
			"recorded once, whatever the round count is (log=%s)\n%v", answers, chatRoundOrderAnswer, path,
			strings.Join(lines, "\n"))
	}
}
