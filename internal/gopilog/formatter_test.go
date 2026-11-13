package gopilog

import (
	"strings"
	"testing"
	"time"
)

// ---- oneLine (BUG-1 regression: multiline task must collapse to one line) ----

func TestOneLine(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"plain", 120, "plain"},
		{"a\nb\nc", 120, "a b c"},
		{"a\r\nb", 120, "a b"},
		{"  spaced   out  ", 120, "spaced out"},
		{"[EPIC GOAL]\nYou are a dev.\n\nBased on this...", 60, "[EPIC GOAL] You are a dev. Based on this..."},
		{"12345678901234567890", 10, "1234567..."},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc"},
		{"", 120, ""},
	}
	for _, c := range cases {
		if got := oneLine(c.in, c.max); got != c.want {
			t.Errorf("oneLine(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestOneLineRuneSafe(t *testing.T) {
	in := "héllo wörld — long text that must be cut"
	got := oneLine(in, 12)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
	if strings.ContainsRune(got, 0xFFFD) {
		t.Errorf("output contains replacement char (mid-rune cut): %q", got)
	}
}

// ---- BUG-1 regression: summary lines stay single-line for multiline tasks ----

func TestFormatSummaryMultilineTask(t *testing.T) {
	s := &Session{
		ID:              "abcdef1234567890",
		Model:           "deepseek-v4-flash",
		Task:            "[EPIC GOAL]\nYou are a dev.\n\nBased on this Epic, do work.",
		ElapsedSec:      12.5,
		ExitCode:        0,
		TotalIterations: 3,
		TotalTokens:     1234,
	}
	out := FormatSummary(s)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("FormatSummary produced %d lines, want 1:\n%s", len(lines), out)
	}
	if strings.Contains(out, "\nYou are") {
		t.Errorf("task newline leaked into summary line:\n%s", out)
	}
}

func TestFormatSummaryTableMultilineTask(t *testing.T) {
	summaries := []SessionSummary{
		{
			SessionID:       "aaaaaaaaaaaaaaaa",
			Model:           "m",
			Task:            "[EPIC GOAL]\nLine two\nLine three",
			ElapsedSec:      1.0,
			ExitCode:        0,
			TotalIterations: 2,
			TotalTokens:     100,
		},
	}
	out := FormatSummaryTable(summaries)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	// header + separator + 1 row
	if len(lines) != 3 {
		t.Errorf("table has %d lines, want 3 (header+sep+row):\n%s", len(lines), out)
	}
	if !strings.Contains(lines[2], "[EPIC GOAL] Line two Line three") {
		t.Errorf("row task not collapsed:\n%s", lines[2])
	}
}

// ---- task list extraction ----

func TestExtractAllTaskLists(t *testing.T) {
	s := &Session{
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{
				"work in progress\n" +
					"[TASK_LIST_BEGIN]\n" +
					`[{"task_id":"1","summary":"first task","status":"progressing","create_at":"1712345678"}]` + "\n" +
					"[TASK_LIST_END]",
			}},
			{Number: 2, TextBlocks: []string{
				"done now\n" +
					"[TASK_LIST_BEGIN]\n" +
					`[{"task_id":"1","summary":"first task","status":"completed","create_at":"1712345678"},` +
					`{"task_id":"2","summary":"second task","status":"completed","create_at":"1712345679"}]` + "\n" +
					"[TASK_LIST_END]",
			}},
		},
	}

	snaps := extractAllTaskLists(s)
	if len(snaps) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snaps))
	}
	if snaps[0].Iteration != 1 || len(snaps[0].Tasks) != 1 {
		t.Errorf("snap0 = %+v", snaps[0])
	}
	if snaps[1].Iteration != 2 || len(snaps[1].Tasks) != 2 {
		t.Errorf("snap1 = %+v", snaps[1])
	}
	if snaps[1].Tasks[1].TaskID != "2" || snaps[1].Tasks[1].Summary != "second task" {
		t.Errorf("snap1 task2 = %+v", snaps[1].Tasks[1])
	}
}

func TestExtractAllTaskListsAlternateFieldsAndNumericIDs(t *testing.T) {
	s := &Session{
		Iterations: []Iteration{
			{Number: 3, TextBlocks: []string{
				"[TASK_LIST_BEGIN]\n" +
					// alternate field names (id/content/created_at) + numeric ID
					`[{"id":1,"content":"alt task","status":"completed","created_at":1712345678}]` + "\n" +
					"[TASK_LIST_END]",
			}},
		},
	}
	snaps := extractAllTaskLists(s)
	if len(snaps) != 1 || len(snaps[0].Tasks) != 1 {
		t.Fatalf("snapshots = %+v", snaps)
	}
	task := snaps[0].Tasks[0]
	if task.TaskID != "1" {
		t.Errorf("TaskID = %q, want \"1\" (numeric ID via flexibleString)", task.TaskID)
	}
	if task.Summary != "alt task" {
		t.Errorf("Summary = %q, want \"alt task\" (alternate field name)", task.Summary)
	}
	if task.CreateAt != "1712345678" {
		t.Errorf("CreateAt = %q, want \"1712345678\" (alternate/numeric)", task.CreateAt)
	}
}

// realFixture68 is the REAL text from session
// 68827372-1b53-4e1f-b21a-92c97bf5f177.jsonl line 281 (same source as
// agent/tasklist_test.go classAFixture68): a complete 7-task JSON block with
// [TASK_LIST_BEGIN] but NO [TASK_LIST_END] — the LLM forgot the closing
// marker. Before the P0 port this session rendered as
// "No task list found in this session." (silent drop).
const realFixture68 = `All deliverables complete. Let me finalize the task list.
[TASK_LIST_BEGIN]
[{"task_id":"1","summary":"研究 loop.go prompt 常量 + gitblame.go 实现 + claude 命令锚点参考","status":"completed"},{"task_id":"2","summary":"修改 git_blame Description 首句为 ALWAYS/NEVER 命令锚点（零密度成本）","status":"completed"},{"task_id":"3","summary":"重建二进制 bin/gopi","status":"completed"},{"task_id":"4","summary":"设计并跑 2 个天然触发 git_blame 的真实任务场景","status":"completed"},{"task_id":"5","summary":"从会话日志验证 git_blame tool_use ≥2 次独立场景","status":"completed"},{"task_id":"6","summary":"go build + go test 全绿（确认唯一失败为 pre-existing flake）","status":"completed"},{"task_id":"7","summary":"逐文件 commit + devdoc 报告","status":"completed"}]`

// ---- task-list BEGIN-without-END recovery (P0 port, devdoc/tasklist-cli-todo-fix.md) ----

func TestExtractAllTaskLists_BeginWithoutEnd_RecoversRealFixture68(t *testing.T) {
	// Regression: session 68827372 line 281 — complete JSON, no END marker.
	// Pre-fix this was silently dropped (endIdx<0 → break); now the
	// balanced-prefix recovery must surface all 7 tasks.
	s := &Session{
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{realFixture68}},
		},
	}
	snaps := extractAllTaskLists(s)
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %d, want 1 (recovered list must not be dropped)", len(snaps))
	}
	if snaps[0].Iteration != 1 {
		t.Errorf("Iteration = %d, want 1", snaps[0].Iteration)
	}
	tasks := snaps[0].Tasks
	if len(tasks) != 7 {
		t.Fatalf("recovered tasks = %d, want 7", len(tasks))
	}
	if tasks[6].TaskID != "7" || tasks[6].Status != "completed" {
		t.Fatalf("unexpected recovered task[6]: %+v", tasks[6])
	}
	if tasks[6].Summary != "逐文件 commit + devdoc 报告" {
		t.Fatalf("unexpected recovered task[6] summary: %+v", tasks[6])
	}
}

func TestExtractAllTaskLists_MixedClosedAndUnclosed(t *testing.T) {
	// Closed block (normal path) + BEGIN-no-END block (recovery path) in the
	// same session: both must be visible, in iteration order, and the normal
	// path must be untouched.
	s := &Session{
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{
				"[TASK_LIST_BEGIN]\n" +
					`[{"task_id":"1","summary":"closed task","status":"progressing"}]` + "\n" +
					"[TASK_LIST_END]",
			}},
			{Number: 2, TextBlocks: []string{
				"[TASK_LIST_BEGIN]\n" +
					`[{"task_id":"1","summary":"closed task","status":"completed"},` +
					`{"task_id":"2","summary":"unclosed task","status":"completed"}]`, // no END
			}},
		},
	}
	snaps := extractAllTaskLists(s)
	if len(snaps) != 2 {
		t.Fatalf("snapshots = %d, want 2 (closed + recovered)", len(snaps))
	}
	if len(snaps[0].Tasks) != 1 || snaps[0].Iteration != 1 {
		t.Errorf("snap0 = %+v, want 1 closed task in iteration 1", snaps[0])
	}
	if len(snaps[1].Tasks) != 2 || snaps[1].Iteration != 2 {
		t.Errorf("snap1 = %+v, want 2 recovered tasks in iteration 2", snaps[1])
	}
	if snaps[1].Tasks[1].TaskID != "2" || snaps[1].Tasks[1].Summary != "unclosed task" {
		t.Errorf("snap1 task2 = %+v", snaps[1].Tasks[1])
	}
}

func TestExtractAllTaskLists_UnrecoverableTail_DropsNotPanic(t *testing.T) {
	// Class B (mid-JSON truncation, never closed) and class C (prose quoting
	// the marker, no JSON): recovery must not fabricate a list and must not
	// panic — the tail is dropped exactly like the closed-block path drops
	// malformed JSON.
	s := &Session{
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{
				"[TASK_LIST_BEGIN]\n" +
					`[{"task_id":"1","summary":"first task","status":"progressing"},` +
					`{"task_id":"2","summary":"truncated task, no closing bracket`,
			}},
			{Number: 2, TextBlocks: []string{
				`The comparison framework is gopi vs claude — including "the mandatory [TASK_LIST_BEGIN] JSON block" in prose.`,
			}},
		},
	}
	snaps := extractAllTaskLists(s)
	if len(snaps) != 0 {
		t.Fatalf("snapshots = %d, want 0 (no garbage list may be fabricated): %+v", len(snaps), snaps)
	}
}

func TestFormatTodo_RecoversBeginWithoutEnd_RealFixture68(t *testing.T) {
	// User-visible regression: `gopi session log <id> --todo` on a session whose
	// task list lacks [TASK_LIST_END] must render the list — NOT
	// "No task list found in this session." (the pre-fix silent drop).
	s := &Session{
		ID:    "68827372",
		Model: "deepseek-v4-flash",
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{realFixture68}},
		},
	}
	out := FormatTodo(s)
	if strings.Contains(out, "No task list found in this session.") {
		t.Fatalf("FormatTodo still reports the silent drop:\n%s", out)
	}
	for _, want := range []string{
		"TASK LIST #1",
		"7 total, 7 completed, 0 progressing",
		"逐文件 commit + devdoc 报告",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTodo missing %q:\n%s", want, out)
		}
	}
}

// ---- formatters ----

func TestFormatSession(t *testing.T) {
	s := &Session{
		ID:         "sess-1",
		Model:      "deepseek-v4-flash",
		Task:       "do the thing",
		ExitCode:   0,
		ElapsedSec: 1.5,
		Iterations: []Iteration{},
	}
	out := FormatSession(s)
	for _, want := range []string{"Session: sess-1", "Model:   deepseek-v4-flash", "TASK:", "EXIT: SUCCESS"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatSession missing %q:\n%s", want, out)
		}
	}
	// THE COST BLOCK IS NOT PART OF A LOG (operator's ruling 2026-11-04): it is
	// CALCULATED from the parsed request rows, so printing it inside a log reads
	// as something the session wrote. The log carries a POINTER to the
	// record-backed view instead — and a pointer, not silence, so a reader can
	// tell "moved" from "lost" (Rule 15). See internal/gopilog/cost.go.
	if strings.Contains(out, CostBlockHeader) {
		t.Errorf("FormatSession must NOT render the cost block (%q) — it is calculated, not logged; see cost.go:\n%s", CostBlockHeader, out)
	}
	if !strings.Contains(out, CostPointerLine()) {
		t.Errorf("FormatSession must carry the cost POINTER line %q:\n%s", CostPointerLine(), out)
	}
}

func TestFormatSessionFailedExit(t *testing.T) {
	s := &Session{ID: "x", ExitCode: 1}
	if !strings.Contains(FormatSession(s), "EXIT: FAILED (exit=1)") {
		t.Errorf("failed exit not rendered")
	}
}

// A session whose log carried no session_end event must NEVER render as a
// success (regression, measured 2026-09-29): a chat SIGKILLed mid tool-call
// printed "EXIT: SUCCESS (exit=0)" because the live-session fallback leaves
// ExitCode at its zero value and the formatter read ExitCode alone. The
// killed run had real work in it — calling that a success is a false claim
// about the one fact this row exists for.
func TestFormatSessionNoEndIsNotSuccess(t *testing.T) {
	s := &Session{ID: "killed", ExitCode: 0, EndUnrecorded: true}
	out := FormatSession(s)
	if strings.Contains(out, "SUCCESS") {
		t.Errorf("a session with no session_end rendered as a success:\n%s", out)
	}
	if !strings.Contains(out, "EXIT: "+sessionNoEndLabel) {
		t.Errorf("no-end session must say %q, got:\n%s", sessionNoEndLabel, out)
	}
}

func TestFormatResponse(t *testing.T) {
	s := &Session{Iterations: []Iteration{
		{Number: 2, TextBlocks: []string{"hello world"}},
	}}
	out := FormatResponse(s, false)
	if !strings.Contains(out, "RESPONSE [iteration 2]:") || !strings.Contains(out, "hello world") {
		t.Errorf("FormatResponse output:\n%s", out)
	}
	// text mode: no label
	outText := FormatResponse(s, true)
	if strings.Contains(outText, "RESPONSE") || !strings.Contains(outText, "hello world") {
		t.Errorf("FormatResponse text mode:\n%s", outText)
	}
}

func TestFormatThinking(t *testing.T) {
	s := &Session{Iterations: []Iteration{
		{Number: 0, ThinkingBlocks: []string{"reason here"}},
	}}
	out := FormatThinking(s, false)
	if !strings.Contains(out, "THINKING [iteration 0]:") || !strings.Contains(out, "reason here") {
		t.Errorf("FormatThinking output:\n%s", out)
	}
}

func TestFormatChat(t *testing.T) {
	s := &Session{
		ID:    "chat-1",
		Model: "m",
		Iterations: []Iteration{
			{
				Number:         0,
				UserPrompt:     "do it",
				ThinkingBlocks: []string{"thinking..."},
				ToolCalls:      []ToolCall{{ID: "c1", Name: "bash", Args: map[string]interface{}{"command": "ls"}}},
				TextBlocks:     []string{"done"},
			},
		},
	}
	out := FormatChat(s)
	for _, want := range []string{"▸ USER", "▸ THINKING", "▸ TOOLS", "▸ RESPONSE", "● bash: ls", "COST SUMMARY"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatChat missing %q:\n%s", want, out)
		}
	}
}

func TestFormatTodo(t *testing.T) {
	s := &Session{
		ID:    "todo-1",
		Model: "m",
		Task:  "epic goal",
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{
				"[TASK_LIST_BEGIN]\n" +
					`[{"task_id":"1","summary":"task one","status":"completed","create_at":"1"}]` + "\n" +
					"[TASK_LIST_END]",
			}},
		},
	}
	out := FormatTodo(s)
	for _, want := range []string{"EPIC GOAL:", "TASK LIST #1 — Iteration 1", "✓ completed", "task one"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTodo missing %q:\n%s", want, out)
		}
	}
}

func TestFormatTodoNoTaskList(t *testing.T) {
	s := &Session{ID: "x", Model: "m"}
	if !strings.Contains(FormatTodo(s), "No task list found") {
		t.Errorf("expected 'No task list found' message")
	}
}

// ---- formatToolCall ----

func TestFormatToolCallBash(t *testing.T) {
	tc := ToolCall{Name: "bash", Args: map[string]interface{}{"command": "ls -la"}, Result: "exit=0\nfiles"}
	out := formatToolCall(1, tc)
	if !strings.Contains(out, "[1] bash: ls -la") {
		t.Errorf("bash call output:\n%s", out)
	}
	if !strings.Contains(out, "GIT COMMIT") {
		// no git commit expected; just ensure no error
	}
	tcGit := ToolCall{Name: "bash", Args: map[string]interface{}{"command": "git commit -m x"}, Result: "git commit\n"}
	if !strings.Contains(formatToolCall(2, tcGit), "GIT COMMIT FOUND") {
		t.Errorf("git commit not highlighted")
	}
}

func TestFormatToolCallGitBlame(t *testing.T) {
	tc := ToolCall{
		Name:   "git_blame",
		Args:   map[string]interface{}{"file_path": "internal/memory/store.go", "start_line": float64(1), "end_line": float64(10)},
		Result: "line 1 → a1b2c3d feat: init",
	}
	out := formatToolCall(1, tc)
	for _, want := range []string{"[1] git_blame: internal/memory/store.go (lines 1-10)", "a1b2c3d"} {
		if !strings.Contains(out, want) {
			t.Errorf("git_blame call output missing %q:\n%s", want, out)
		}
	}
	// args absent → no panic, just file-less line
	tcNoArgs := ToolCall{Name: "git_blame", Result: "line 1"}
	if !strings.Contains(formatToolCall(2, tcNoArgs), "git_blame:") {
		t.Errorf("git_blame with no args should still render:\n%s", formatToolCall(2, tcNoArgs))
	}
}

// ---- --filter / --memory tool view ----

func TestFormatToolFilterMemory(t *testing.T) {
	s := &Session{
		ID:    "f-1",
		Model: "m",
		Iterations: []Iteration{
			{
				Number: 0,
				ToolCalls: []ToolCall{
					{ID: "call_s1", Name: "memory_search", Args: map[string]interface{}{"query": "prev"}, Result: "hit", Timestamp: mustParseTime(t, "2026-08-10T09:00:02Z")},
					{ID: "call_b1", Name: "bash", Args: map[string]interface{}{"command": "ls"}, Result: "exit=0"},
					{ID: "call_fb1", Name: "memory_feedback", Args: map[string]interface{}{"id": float64(1), "rating": "useful"}, Result: "Feedback recorded (id=1)."},
					{ID: "call_st1", Name: "memory_store", Args: map[string]interface{}{"answer": "ans"}, Result: "stored"},
					{ID: "call_up1", Name: "memory_update", Args: map[string]interface{}{"id": float64(1), "keywords": []interface{}{"kw"}}, Result: "Memory updated (id=1)."},
				},
			},
		},
	}
	out := FormatToolFilter(s, map[string]bool{"memory_search": true, "memory_store": true, "memory_feedback": true, "memory_update": true})
	for _, want := range []string{"Filter:  memory_feedback, memory_search, memory_store, memory_update", "memory_search", "memory_store", "memory_feedback", "memory_update", "call_s1", "call_st1", "call_fb1", "call_up1"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter(f, memory) missing %q:\n%s", want, out)
		}
	}
	// bash call must be excluded by the memory filter
	if strings.Contains(out, "call_b1") || strings.Contains(out, "● bash") {
		t.Errorf("bash call leaked into memory filter output:\n%s", out)
	}
}

// TestFormatToolFilterVision guards the image-processing filter: vision_analyze
// calls must be selectable by name (the --vision family shortcut expands to
// this name), and non-vision calls must stay excluded.
func TestFormatToolFilterVision(t *testing.T) {
	s := &Session{
		ID:    "f-vision",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, ToolCalls: []ToolCall{
				{ID: "call_vis1", Name: "vision_analyze", Args: map[string]interface{}{"image_path": "/tmp/x.png", "prompt": "识别文字"}, Result: "【vision 分析结果】...\n这是一张截图"},
				{ID: "call_b1", Name: "bash", Args: map[string]interface{}{"command": "ls"}, Result: "exit=0"},
			}},
		},
	}
	out := FormatToolFilter(s, map[string]bool{"vision_analyze": true})
	for _, want := range []string{"Filter:  vision_analyze", "call_vis1", "/tmp/x.png", "vision 分析结果"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter(vision) missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "call_b1") {
		t.Errorf("bash call leaked into vision filter output:\n%s", out)
	}
}

// TestFormatToolFilterText2img guards the rendering filter: text2img calls
// must be selectable by name (the --text2img family shortcut expands to this
// name), and non-text2img calls must stay excluded.
func TestFormatToolFilterText2img(t *testing.T) {
	s := &Session{
		ID:    "f-t2i",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, ToolCalls: []ToolCall{
				{ID: "call_t2i1", Name: "text2img", Args: map[string]interface{}{"src": "/tmp/notes.txt", "out": "/tmp/notes.png", "size": float64(20)}, Result: "✅ /tmp/notes.txt → 1 页"},
				{ID: "call_b1", Name: "bash", Args: map[string]interface{}{"command": "ls"}, Result: "exit=0"},
			}},
		},
	}
	out := FormatToolFilter(s, map[string]bool{"text2img": true})
	for _, want := range []string{"Filter:  text2img", "call_t2i1", "/tmp/notes.txt", "/tmp/notes.png", "1 页"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter(text2img) missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "call_b1") {
		t.Errorf("bash call leaked into text2img filter output:\n%s", out)
	}
}

// TestFormatToolFilterGitBlame guards the git-history filter: git_blame calls
// must be selectable by name (the --git family shortcut expands to this name),
// and non-git_blame calls must stay excluded.
func TestFormatToolFilterGitBlame(t *testing.T) {
	s := &Session{
		ID:    "f-git",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, ToolCalls: []ToolCall{
				{ID: "call_gb1", Name: "git_blame", Args: map[string]interface{}{"file_path": "internal/memory/store.go", "start_line": float64(1), "end_line": float64(10)}, Result: "line 1 → a1b2c3d feat: ..."},
				{ID: "call_b1", Name: "bash", Args: map[string]interface{}{"command": "ls"}, Result: "exit=0"},
			}},
		},
	}
	out := FormatToolFilter(s, map[string]bool{"git_blame": true})
	for _, want := range []string{"Filter:  git_blame", "call_gb1", "internal/memory/store.go", "a1b2c3d"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter(git_blame) missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "call_b1") {
		t.Errorf("bash call leaked into git_blame filter output:\n%s", out)
	}
}

func TestFormatToolFilterBashGeneric(t *testing.T) {
	s := &Session{
		ID:    "f-2",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, ToolCalls: []ToolCall{
				{ID: "call_b1", Name: "bash", Args: map[string]interface{}{"command": "ls"}, Result: "exit=0"},
				{ID: "call_r1", Name: "read", Args: map[string]interface{}{"path": "go.mod"}, Result: "module..."},
			}},
		},
	}
	out := FormatToolFilter(s, map[string]bool{"bash": true})
	if !strings.Contains(out, "call_b1") {
		t.Errorf("bash filter should include call_b1:\n%s", out)
	}
	if strings.Contains(out, "call_r1") {
		t.Errorf("read call leaked into bash filter output:\n%s", out)
	}
}

func TestFormatToolFilterNoMatch(t *testing.T) {
	s := &Session{ID: "f-3", Model: "m", Iterations: []Iteration{
		{Number: 0, ToolCalls: []ToolCall{{ID: "c", Name: "bash"}}},
	}}
	out := FormatToolFilter(s, map[string]bool{"memory_search": true})
	if !strings.Contains(out, "No matching tool calls found") {
		t.Errorf("expected no-match message:\n%s", out)
	}
}

func TestFormatToolFilterEmptyNames(t *testing.T) {
	s := &Session{ID: "f-4", Model: "m"}
	out := FormatToolFilter(s, map[string]bool{})
	if !strings.Contains(out, "no tool filter names given") {
		t.Errorf("expected empty-names message:\n%s", out)
	}
}

func TestFormatToolFilterFullInputIndentAndRaw(t *testing.T) {
	raw := jsonMsg(`{"query":"q1","answer":"a1"}`)
	s := &Session{ID: "f-5", Model: "m", Iterations: []Iteration{
		{Number: 0, ToolCalls: []ToolCall{
			{ID: "c1", Name: "memory_search", InputRaw: raw, Result: "res", Timestamp: mustParseTime(t, "2026-08-10T09:00:02Z")},
		}},
	}}
	out := FormatToolFilter(s, map[string]bool{"memory_search": true})
	// tool_use_id header + hex timestamp + indented input fields + result
	for _, want := range []string{"tool_use_id: c1", "09:00:02", `"query": "q1"`, `"answer": "a1"`, "result:", "res"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter full-input missing %q:\n%s", want, out)
		}
	}
}

// TestFormatToolInputNoHTMLEscape is a regression test for the --memory /
// --filter view: Go's encoding/json HTML-escapes < > & into \u003c \u003e
// \u0026 on both MarshalIndent and Encoder.Encode unless disabled. The filtered
// view must render the literal characters the LLM actually sent, and preserve
// the original key order of the tool_use.input JSON.
func TestFormatToolInputNoHTMLEscape(t *testing.T) {
	raw := jsonMsg(`{"topic":"A > B & C < D","answer":"x \u003e y","keywords":["k1","a & b"],"n":3,"ok":true,"list":[1,2],"nested":{"z":"a < b"}}`)
	s := &Session{ID: "f-6", Model: "m", Iterations: []Iteration{
		{Number: 0, ToolCalls: []ToolCall{
			{ID: "c1", Name: "memory_store", InputRaw: raw, Result: "stored"},
		}},
	}}
	out := FormatToolFilter(s, map[string]bool{"memory_store": true})

	// HTML-significant characters must NOT be re-escaped.
	for _, escaped := range []string{`\u003e`, `\u0026`, `\u003c`, `\u003E`, `\u0026`} {
		if strings.Contains(out, escaped) {
			t.Errorf("FormatToolFilter HTML-escaped %q — should render literal char:\n%s", escaped, out)
		}
	}
	// Literal characters present.
	for _, want := range []string{`"A > B & C < D"`, `"x > y"`, `"a & b"`, `"a < b"`} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter missing literal %q:\n%s", want, out)
		}
	}
	// Scalar values survive (number, bool, array, nested object).
	for _, want := range []string{`"n": 3`, `"ok": true`, `"z": "a < b"`, `1`, `2`} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatToolFilter missing value %q:\n%s", want, out)
		}
	}
	// Key order preserved exactly as logged.
	ks := []string{`"topic"`, `"answer"`, `"keywords"`, `"n"`, `"ok"`, `"list"`, `"nested"`}
	prev := -1
	for _, k := range ks {
		idx := strings.Index(out, k)
		if idx < 0 {
			t.Errorf("FormatToolFilter missing key %q:\n%s", k, out)
			continue
		}
		if idx < prev {
			t.Errorf("FormatToolFilter key %q out of order:\n%s", k, out)
		}
		prev = idx
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

// ---------------------------------------------------------------------------
// FormatGrep — content search over a session (the log-side keyword_search).
// ---------------------------------------------------------------------------

func grepFixtureSession() *Session {
	return &Session{
		ID:    "g-1",
		Model: "m",
		Iterations: []Iteration{
			{
				Number: 1,
				ToolCalls: []ToolCall{
					{ID: "call_cs1", Name: "code_search", Args: map[string]interface{}{"symbol": "NewClient"}, Result: "Symbol: NewClient\ninternal/tools/read.go:42"},
					{ID: "call_ws1", Name: "web_search", Args: map[string]interface{}{"query": "go modules"}, Result: `{"status":"ok","count":5}`},
					{ID: "call_b1", Name: "bash", Args: map[string]interface{}{"command": "go build"}, Result: "exit=0"},
				},
				TextBlocks:     []string{"NewClient is defined in internal/tools."},
				ThinkingBlocks: []string{"The build passed quickly."},
			},
		},
	}
}

func TestFormatGrepMatchesToolAndText(t *testing.T) {
	out := FormatGrep(grepFixtureSession(), "NewClient", nil, false)
	for _, want := range []string{
		`Grep:    "NewClient" (all content)`,
		"TOOL 1/2: code_search",
		"call_cs1",
		"internal/tools/read.go:42",
		"TEXT 2/2: iteration 1",
		"NewClient is defined in internal/tools.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatGrep missing %q:\n%s", want, out)
		}
	}
	// Non-matching entries (bash, thinking) must not appear.
	if strings.Contains(out, "call_b1") || strings.Contains(out, "call_ws1") || strings.Contains(out, "build passed") {
		t.Errorf("non-matching entries leaked into grep output:\n%s", out)
	}
}

func TestFormatGrepCaseInsensitiveAndScoped(t *testing.T) {
	// Case-insensitive: "newclient" matches "NewClient" text and tool input.
	out := FormatGrep(grepFixtureSession(), "newclient", nil, false)
	if !strings.Contains(out, "code_search") || !strings.Contains(out, "TEXT") {
		t.Errorf("case-insensitive grep missed matches:\n%s", out)
	}

	// Scoped to the web family: only web_search matches, text is ignored.
	out = FormatGrep(grepFixtureSession(), "modules", map[string]bool{"web_search": true, "web_fetch": true}, false)
	if !strings.Contains(out, "web_search") {
		t.Errorf("scoped grep missing web_search:\n%s", out)
	}
	if strings.Contains(out, "TEXT") || strings.Contains(out, "code_search") {
		t.Errorf("scoped grep leaked non-web entries:\n%s", out)
	}
}

func TestFormatGrepNoMatch(t *testing.T) {
	out := FormatGrep(grepFixtureSession(), "nonexistent-term-xyz", nil, false)
	if !strings.Contains(out, `No entries in this session contain "nonexistent-term-xyz".`) {
		t.Errorf("no-match message wrong:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// formatChatToolCall — search-family tools get a query + summary line.
// ---------------------------------------------------------------------------

func TestFormatChatSearchTools(t *testing.T) {
	s := &Session{
		ID:    "c-1",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, ToolCalls: []ToolCall{
				{Name: "code_search", Args: map[string]interface{}{"symbol": "NewClient"}, Result: "Symbol: NewClient\nfile:line"},
				{Name: "keyword_search", Args: map[string]interface{}{"query": "timeout"}, Result: "hit1\nhit2"},
				{Name: "web_search", Args: map[string]interface{}{"query": "go modules"}, Result: `{"status":"ok","count":5}`},
				{Name: "web_fetch", Args: map[string]interface{}{"url": "https://example.com"}, Result: "page text"},
				{Name: "bash", Args: map[string]interface{}{"command": "ls"}, Result: "exit=0"},
			}},
		},
	}
	out := FormatChat(s)
	for _, want := range []string{
		"● code_search: NewClient",
		"→ Symbol: NewClient",
		"● keyword_search: timeout",
		"● web_search: go modules",
		`● web_fetch: https://example.com`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatChat missing %q:\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// BUG-1 regression: rune-safe truncation. Byte slicing of CJK text cut
// multi-byte runes mid-sequence, producing U+FFFD replacement chars in
// output. Every display truncation must be rune-safe.
// ---------------------------------------------------------------------------

// cjkText builds a CJK string long enough to cross every truncation cap.
func cjkText(runes int) string {
	return strings.Repeat("知", runes)
}

// assertNoReplacementChar fails if the output contains a U+FFFD replacement
// char (the signature of a mid-rune byte cut).
func assertNoReplacementChar(t *testing.T, label, out string) {
	t.Helper()
	if strings.ContainsRune(out, 0xFFFD) {
		t.Errorf("%s: output contains U+FFFD (mid-rune byte cut):\n%s", label, out)
	}
}

// TestFormatSessionCJKNoReplacementChar — FormatSession's text block
// truncation (formerly text[:300]) must not split a rune.
func TestFormatSessionCJKNoReplacementChar(t *testing.T) {
	s := &Session{
		ID:    "cjk-session",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, TextBlocks: []string{cjkText(400)}},
		},
	}
	assertNoReplacementChar(t, "FormatSession", FormatSession(s))
}

// TestFormatChatCJKNoReplacementChar — FormatChat's thinking block
// truncation (formerly combined[:500]) must not split a rune.
func TestFormatChatCJKNoReplacementChar(t *testing.T) {
	s := &Session{
		ID:    "cjk-chat",
		Model: "m",
		Iterations: []Iteration{
			{Number: 1, ThinkingBlocks: []string{cjkText(600)}},
		},
	}
	assertNoReplacementChar(t, "FormatChat", FormatChat(s))
}

// TestFormatToolCallCJKNoReplacementChar — bash result first-line truncation
// (formerly summary[:77]) and edit old/new preview (formerly [:57]) must not
// split runes.
func TestFormatToolCallCJKNoReplacementChar(t *testing.T) {
	s := &Session{
		Iterations: []Iteration{{
			Number: 1,
			ToolCalls: []ToolCall{
				{
					Name:   "bash",
					Args:   map[string]interface{}{"command": "echo"},
					Result: cjkText(200) + "\nsecond line",
				},
				{
					Name:   "edit",
					Args:   map[string]interface{}{"path": "f.go", "old_string": cjkText(100), "new_string": cjkText(100)},
					Result: "ok",
				},
			},
		}},
	}
	out := FormatSession(s)
	assertNoReplacementChar(t, "FormatToolCall", out)
	// The truncated first line carries the "..." marker (bash shows only the
	// first result line by design).
	if !strings.Contains(out, "...") {
		t.Errorf("FormatToolCall missing truncation marker:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// BUG-6 regression: --json refuses empty / no-session_start files with a
// clear error instead of printing raw garbage or nothing.
// ---------------------------------------------------------------------------

func TestValidateSessionFileOK(t *testing.T) {
	path := writeFixture(t, []string{
		`{"type":"session_start","sessionId":"s1","timestamp":"2026-08-06T10:00:00Z"}`,
		`{"type":"user","message":{"content":"hi","role":"user"},"timestamp":"2026-08-06T10:00:01Z"}`,
	})
	if err := ValidateSessionFile(path); err != nil {
		t.Fatalf("ValidateSessionFile on valid session: %v", err)
	}
}

func TestValidateSessionFileEmpty(t *testing.T) {
	path := writeFixture(t, nil)
	err := ValidateSessionFile(path)
	if err == nil {
		t.Fatal("ValidateSessionFile on empty file: expected error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty-file error must mention empty, got: %v", err)
	}
}

func TestValidateSessionFileNoSessionStart(t *testing.T) {
	path := writeFixture(t, []string{
		`{"type":"user","message":{"content":"hi","role":"user"},"timestamp":"2026-08-06T10:00:01Z"}`,
	})
	err := ValidateSessionFile(path)
	if err == nil {
		t.Fatal("ValidateSessionFile on no-session_start file: expected error")
	}
	if !strings.Contains(err.Error(), "session_start") {
		t.Errorf("no-session_start error must mention session_start, got: %v", err)
	}
}

func TestFormatJSONLRefusesEmpty(t *testing.T) {
	path := writeFixture(t, nil)
	if _, err := FormatJSONL(path); err == nil {
		t.Fatal("FormatJSONL on empty file: expected error")
	}
}

// ---------------------------------------------------------------------------
// FormatGrep — USER ROWS (2026-10-31 regression)
//
// MEASURED DEFECT THIS PINS: until this change FormatGrep walked only
// Iterations' tool calls, text and thinking. A `user` row — the operator's own
// message and the harness's injections — was in session.jsonl, in
// Iteration.UserPrompt and in Iteration.HarnessPrompts, and NO --grep query
// could reach it. On session 01a18f0f-2a05-71b5-8ceb-5e7a0dfe1cd3 the phrase
// "EPIC GOAL" occurs in the run's own briefing (a user row) while
// `--grep "EPIC GOAL"` answered "No entries in this session contain" it.
// A content search that skips half the conversation is the one defect this
// surface exists to prevent, so it gets a test rather than a comment.
// ---------------------------------------------------------------------------

func grepPromptFixtureSession() *Session {
	return &Session{
		ID:    "g-prompt",
		Model: "m",
		Iterations: []Iteration{
			{
				Number:         1,
				UserPrompt:     "please re-read what I said about the resume algorithm",
				HarnessPrompts: []string{"[system] [t+9s] — a clock tick row"},
				TextBlocks:     []string{"the resume algorithm is in internal/agent/resume.go"},
			},
			{
				Number:     2,
				UserPrompt: "unrelated follow-up",
				ToolCalls: []ToolCall{
					{ID: "call_x", Name: "read", Args: map[string]interface{}{"path": "resume.go"}, Result: "package agent"},
				},
			},
		},
	}
}

func TestFormatGrepFindsOperatorPromptRows(t *testing.T) {
	out := FormatGrep(grepPromptFixtureSession(), "resume algorithm", nil, false)
	if !strings.Contains(out, grepPromptTurnLabel) {
		t.Fatalf("the OPERATOR's own row is not searched — no %q hit:\n%s", grepPromptTurnLabel, out)
	}
	if !strings.Contains(out, "please re-read what I said about the resume algorithm") {
		t.Errorf("operator row text missing from the hit:\n%s", out)
	}
	// The text block that also matched must still be there: prompt rows are
	// ADDED to the search, never a replacement for it.
	if !strings.Contains(out, "internal/agent/resume.go") {
		t.Errorf("text-block matching regressed when prompt rows were added:\n%s", out)
	}
}

func TestFormatGrepFindsHarnessPromptRows(t *testing.T) {
	out := FormatGrep(grepPromptFixtureSession(), "clock tick", nil, false)
	if !strings.Contains(out, grepPromptHarnessLabel) {
		t.Fatalf("harness-injected rows are not searched — no %q hit:\n%s", grepPromptHarnessLabel, out)
	}
	if !strings.Contains(out, "a clock tick row") {
		t.Errorf("harness row text missing from the hit:\n%s", out)
	}
}

// A --search/--web … tool scope asks "which TOOL CALL said X", so it must not
// start returning prompt rows: the scope's meaning is tool calls.
func TestFormatGrepToolScopeStillSuppressesPromptRows(t *testing.T) {
	out := FormatGrep(grepPromptFixtureSession(), "resume algorithm", map[string]bool{"read": true}, false)
	if strings.Contains(out, grepPromptTurnLabel) {
		t.Errorf("a tool scope leaked prompt rows into a tool-only search:\n%s", out)
	}
	if strings.Contains(out, "please re-read") {
		t.Errorf("a tool scope leaked operator text:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// FormatGrep — the match WINDOW (2026-10-31 regression)
//
// MEASURED DEFECT THIS PINS: the surface rendered every matched field in FULL.
// `--grep "timed out after 1s"` on session
// 004c3578-37f7-4e57-852f-b9c362e5c460 printed 19393 bytes, of which 17933 were
// the `content` argument of the write call that matched — 99.6% of the answer
// was prose the reader never asked for, and the needle was buried in it.
// ---------------------------------------------------------------------------

func TestGrepWindowCapsAndKeepsTheMatch(t *testing.T) {
	// A very large field whose ONLY match sits past the middle.
	big := strings.Repeat("filler line that carries no claim. ", 3000) + "THE NEEDLE" +
		strings.Repeat(" trailing filler.", 3000)
	out := grepWindow(big, "the needle")
	if n := len([]rune(out)); n > grepMatchWindowRunes+200 {
		t.Errorf("window did not cap the field: %d runes (cap %d)", n, grepMatchWindowRunes)
	}
	if !strings.Contains(out, "THE NEEDLE") {
		t.Fatalf("window DROPPED the match it exists to show:\n%s", out[:min(400, len(out))])
	}
	// The cut is STATED, so a reader cannot mistake a window for the whole field.
	if !strings.Contains(out, "runes total") {
		t.Errorf("a truncated window must state its total, got:\n%s", out)
	}
}

func TestGrepWindowLeavesSmallFieldsAlone(t *testing.T) {
	small := "exit=1\nerror: the connection was refused"
	if got := grepWindow(small, "connection"); got != small {
		t.Errorf("a field that fits the window must be unchanged, got:\n%q", got)
	}
}

// Rune-safe: a CJK match must not be sliced mid-rune (the same defect the
// byte-slicing previews in this file were reviewed for).
func TestGrepWindowIsRuneSafeOnCJK(t *testing.T) {
	// Pad so the match straddles the window's own start boundary.
	big := strings.Repeat("填充文字", 400) + "关键匹配词" + strings.Repeat("尾巴文字", 400)
	out := grepWindow(big, "关键匹配词")
	if !strings.Contains(out, "关键匹配词") {
		t.Fatalf("CJK match lost from the window:\n%s", out)
	}
	if strings.ContainsRune(out, '\uFFFD') {
		t.Errorf("window sliced a UTF-8 rune mid-sequence (U+FFFD present):\n%s", out)
	}
}

// The whole ANSWER is bounded, not just one field: input AND result are
// windowed, so a hit on a huge argument cannot re-inflate the output.
func TestFormatGrepBoundsBothFieldsOfOneHit(t *testing.T) {
	hugeArgs := strings.Repeat("x", 40000)
	s := &Session{
		ID: "g-huge",
		Iterations: []Iteration{{
			Number: 1,
			ToolCalls: []ToolCall{{
				ID:     "call_huge",
				Name:   "write",
				Args:   map[string]interface{}{"content": hugeArgs},
				Result: "ERROR: the disk is full " + strings.Repeat("y", 40000),
			}},
		}},
	}
	out := FormatGrep(s, "ERROR: the disk is full", nil, false)
	if len(out) > 4*grepMatchWindowRunes {
		t.Errorf("one hit rendered %d bytes for a ~200-byte answer (window %d)",
			len(out), grepMatchWindowRunes)
	}
	if !strings.Contains(out, "ERROR: the disk is full") {
		t.Errorf("the match itself was dropped:\n%s", out[:min(500, len(out))])
	}
	// Which field matched is named — with two windows open the reader would
	// otherwise have to diff them by eye.
	if !strings.Contains(out, grepMatchFieldResults) {
		t.Errorf("the renderer does not say which field matched:\n%s", out[:min(500, len(out))])
	}
}
