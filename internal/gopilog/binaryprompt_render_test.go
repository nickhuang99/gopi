package gopilog

// Regression guards for the gopilog half of the binary-briefing defect: the
// session renderers printed s.Task — the session's accepted prompt, i.e. the
// FIRST user row — verbatim, so a session whose prompt WAS a binary file (a
// bare-prompt run whose positional named an existing non-image file: measured,
// the run's ./gopi ELF executable) rendered `data` on the terminal:
// `gopi session log <id> --todo` exited 0 and emitted the ELF header + NUL bytes.
//
// The fixture is the REAL byte prefix of that session's prompt, not a synthetic
// "\x00\x01" stand-in (PROJECT.md Rule 10: verify on the smallest real sample).
// Assertions are made on the RENDERED STRING — the exact bytes the CLI writes —
// never on an intermediate value, because the defect IS a rendered-output
// property (Rule 10.2: render truth).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// nulByte is the byte `file(1)` uses to decide a file is "data" rather than
// text — the single byte this whole guard exists to keep off the rendered line.
var nulByte = []byte{0}

// elfPromptFixtureBytes is the captured-prompt size the render regressions
// use: large enough to be a real blob (so the guard's blob form fires — the
// shape the measured 21 MB briefing had), small enough to stay a readable
// failure message.
const elfPromptFixtureBytes = 200

// elfPromptHead is the head of the ELF64 executable that became a prompt.
var elfPromptHead = []byte{
	0x7f, 'E', 'L', 'F',
	0x02, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x3e, 0x00, 0x01, 0x00,
}

// binaryPromptTask is the shape of the session's first user row: the EPIC GOAL
// marker followed by ~200 bytes of the executable (what the real session held).
func binaryPromptTask(n int) string {
	var b []byte
	for len(b) < n {
		b = append(b, elfPromptHead...)
	}
	return "[EPIC GOAL]\n" + string(b[:n]) + "\nrun it"
}

// assertTextOutput is the oracle for every path below: the rendered output must
// contain no NUL byte (what `file -` calls "data" and `read` refuses) and must
// be valid UTF-8. When the path actually prints the captured task
// (wantEvidence), the magic bytes must still be VISIBLE — the honest escape
// (Rule 15: a sanitizer that silently dropped the payload would destroy the
// only clue about what was captured).
func assertTextOutput(t *testing.T, path, out string, wantEvidence bool) {
	t.Helper()
	if n := bytes.Count([]byte(out), nulByte); n != 0 {
		t.Errorf("%s: rendered output carries %d NUL byte(s) — `file -` would call it data\n%q", path, n, out)
	}
	if !utf8.ValidString(out) {
		t.Errorf("%s: rendered output is not valid UTF-8", path)
	}
	if !wantEvidence {
		return
	}
	if !strings.Contains(out, "\\x7fELF") {
		t.Errorf("%s: the ELF magic bytes must stay VISIBLE as evidence, got:\n%s", path, out)
	}
	if !strings.Contains(out, "[bin ") {
		t.Errorf("%s: escaped run must be marked, got:\n%s", path, out)
	}
}

// TestFormatSessionAndTodoNeverRenderBinaryTask is the per-path regression the
// epic asks for: EVERY s.Task emitter in formatter.go — FormatSession (TASK:
// block) and FormatTodo (EPIC GOAL: block) — must render text for a binary
// briefing.
func TestFormatSessionAndTodoNeverRenderBinaryTask(t *testing.T) {
	s := &Session{ID: "fixture-session", Model: "test-model", Task: binaryPromptTask(200)}
	assertTextOutput(t, "FormatSession", FormatSession(s), true)
	assertTextOutput(t, "FormatTodo", FormatTodo(s), true)
	// FormatChat prints s.Task only through iteration 0's UserPrompt (built by
	// the reader) — covered by the parsed-session test below; with no
	// iterations the output carries no task text at all.
	assertTextOutput(t, "FormatChat (no iterations)", FormatChat(s), false)
}

// TestSummaryRowsNeverRenderBinaryTask covers the single-line emitters, which
// build their own string (FormatSummary, FormatSummaryTable, and the
// SessionSummary rows LoadAllSummaries fills for `gopi session log --last`).
func TestSummaryRowsNeverRenderBinaryTask(t *testing.T) {
	s := &Session{ID: "fixture-session", Model: "test-model", Task: binaryPromptTask(200)}
	assertTextOutput(t, "FormatSummary", FormatSummary(s), true)
	assertTextOutput(t, "FormatSummaryTable", FormatSummaryTable([]SessionSummary{{
		Path: "p", SessionID: "fixture-session", Model: "test-model",
		Task: binaryPromptTask(summaryTaskMaxLen),
	}}), true)
}

// TestSessionTaskGuardIsIdentityForText pins the contract that makes this guard
// safe to apply at render time: ordinary text — CJK briefings included — passes
// through byte-for-byte, so no legitimate session view changes.
func TestSessionTaskGuardIsIdentityForText(t *testing.T) {
	for _, task := range []string{
		"[EPIC GOAL]\n修复 merge 冲突：fix/cwd-and-lifetime（背景：the operator 的 harvest 是否也 push HP 分支）",
		"single line task with emoji 🙂 and symbols → ✓",
		"line one\nline two\n\tindented",
	} {
		if got := sessionTaskText(task); got != task {
			t.Errorf("sessionTaskText(%q) = %q, want identity", task, got)
		}
		if got := sessionTaskLines(task); strings.Join(got, "\n") != task {
			t.Errorf("sessionTaskLines(%q) = %q, want identity join", task, got)
		}
	}
}

// TestParseAndRenderBinaryPromptSession is the END-TO-END regression: a real
// session FILE whose first user row carries the ELF header (exactly how the
// 87 MB session began, JSON-escaped the way the logger writes it) is parsed and
// rendered with no NUL byte reaching the output of any view.
func TestParseAndRenderBinaryPromptSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	start := RawEvent{Type: sessionevent.EventTypeSessionStart}
	start.SessionID = "fixture-binary-prompt"
	start.Model = "test-model"
	start.Workdir = "/home/user/workspace/peak-bin"

	writeJSONLine(t, path, start)
	writeJSONLine(t, path, userEvent(binaryPromptTask(200)))
	writeJSONLine(t, path, assistantEvent("working on it"))

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", path, err)
	}
	if !strings.Contains(s.Task, "\x7fELF") {
		t.Fatalf("fixture did not reach s.Task — got %q", s.Task)
	}
	for name, out := range map[string]string{
		"FormatSession": FormatSession(s),
		"FormatTodo":    FormatTodo(s),
		"FormatChat":    FormatChat(s),
		"FormatSummary": FormatSummary(s),
	} {
		assertTextOutput(t, name+" (parsed session)", out, true)
	}
}

func writeJSONLine(t *testing.T, path string, ev RawEvent) {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open session file: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		t.Fatalf("write session line: %v", err)
	}
}

func userEvent(content string) RawEvent {
	ev := RawEvent{Type: "user"}
	ev.Message.Content = mustJSON(content)
	return ev
}

func assistantEvent(text string) RawEvent {
	ev := RawEvent{Type: "assistant"}
	ev.Message.Content = mustJSON([]map[string]string{{"type": "text", "text": text}})
	return ev
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
