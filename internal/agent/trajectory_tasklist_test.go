package agent

import (
	"fmt"
	"strconv"
	"testing"
)

// Task-list telemetry fixtures (P4). Each builds a synthetic session JSONL and
// asserts the four new TrajectoryStats fields:
//
//	FirstTasklistMsgRatio, NLists, NMalformed, ListEverEstablished.
//
// A "list message" is an assistant text block carrying both
// [TASK_LIST_BEGIN] and [TASK_LIST_END]; BEGIN without END is the silent-drop
// malformed class (P0) and counts into NMalformed only.

const (
	tlBegin = "[TASK_LIST_BEGIN]"
	tlEnd   = "[TASK_LIST_END]"
)

// tlList renders a minimal valid task-list text block.
func tlList() string {
	return tlBegin + "\n[]\n" + tlEnd
}

// jqText JSON-escapes s for embedding as a single-line JSON string value
// (the session scanner splits on newlines, so the text must not contain raw
// line breaks — strconv.Quote renders \n as "\n").
func jqText(s string) string {
	return strconv.Quote(s)
}

// TestTrajTaskList_NoListNever establishes a list → ratio -1, ever=false.
func TestTrajTaskList_NoList(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reading files"}]},"timestamp":%q}`, ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"all done"}]},"timestamp":%q}`, ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(5)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(6)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.ListEverEstablished {
		t.Errorf("ListEverEstablished = true, want false")
	}
	if stats.NLists != 0 {
		t.Errorf("NLists = %d, want 0", stats.NLists)
	}
	if stats.NMalformed != 0 {
		t.Errorf("NMalformed = %d, want 0", stats.NMalformed)
	}
	if stats.FirstTasklistMsgRatio != -1 {
		t.Errorf("FirstTasklistMsgRatio = %v, want -1 (no list)", stats.FirstTasklistMsgRatio)
	}
}

// TestTrajTaskList_ListAtStart: list on the first text message (index 0).
func TestTrajTaskList_ListAtStart(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlList()), ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"progress"}]},"timestamp":%q}`, ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(5)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(6)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if !stats.ListEverEstablished {
		t.Errorf("ListEverEstablished = false, want true")
	}
	if stats.NLists != 1 {
		t.Errorf("NLists = %d, want 1", stats.NLists)
	}
	if stats.NMalformed != 0 {
		t.Errorf("NMalformed = %d, want 0", stats.NMalformed)
	}
	// first list at index 0 / 2 text messages = 0.0
	if stats.FirstTasklistMsgRatio != 0 {
		t.Errorf("FirstTasklistMsgRatio = %v, want 0.0", stats.FirstTasklistMsgRatio)
	}
}

// TestTrajTaskList_ListAtEnd: the ONLY list is on the final message.
// 3 text messages total; list at index 2 → ratio 2/3.
func TestTrajTaskList_ListAtEnd(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"step 1"}]},"timestamp":%q}`, ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"step 2"}]},"timestamp":%q}`, ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(5)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlList()), ts(6)),
		fmt.Sprintf(`{"type":"request","iteration":2,"completion_tokens":10,"timestamp":%q}`, ts(7)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(8)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if !stats.ListEverEstablished {
		t.Errorf("ListEverEstablished = false, want true")
	}
	if stats.NLists != 1 {
		t.Errorf("NLists = %d, want 1", stats.NLists)
	}
	if stats.FirstTasklistMsgRatio != 2.0/3.0 {
		t.Errorf("FirstTasklistMsgRatio = %v, want %v (2/3)", stats.FirstTasklistMsgRatio, 2.0/3.0)
	}
}

// TestTrajTaskList_MalformedBeginNoEnd: BEGIN without END is the silent-drop
// class → NMalformed=1, NOT a valid list (NLists stays 0, ever=false).
func TestTrajTaskList_MalformedBeginNoEnd(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlBegin+" truncated"), ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]},"timestamp":%q}`, ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(5)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(6)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.NMalformed != 1 {
		t.Errorf("NMalformed = %d, want 1", stats.NMalformed)
	}
	if stats.ListEverEstablished {
		t.Errorf("ListEverEstablished = true, want false (malformed drop)")
	}
	if stats.NLists != 0 {
		t.Errorf("NLists = %d, want 0 (malformed is not a list)", stats.NLists)
	}
	if stats.FirstTasklistMsgRatio != -1 {
		t.Errorf("FirstTasklistMsgRatio = %v, want -1", stats.FirstTasklistMsgRatio)
	}
}

// TestTrajTaskList_MixedCompletesList: 3 text messages: malformed, valid list
// (mid), final. NLists=1, NMalformed=1, ever=true, first list at index 1 → 1/3.
func TestTrajTaskList_MixedCompletesList(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlBegin+" no end"), ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlList()), ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(5)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"complete"}]},"timestamp":%q}`, ts(6)),
		fmt.Sprintf(`{"type":"request","iteration":2,"completion_tokens":10,"timestamp":%q}`, ts(7)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(8)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.NMalformed != 1 {
		t.Errorf("NMalformed = %d, want 1", stats.NMalformed)
	}
	if stats.NLists != 1 {
		t.Errorf("NLists = %d, want 1", stats.NLists)
	}
	if !stats.ListEverEstablished {
		t.Errorf("ListEverEstablished = false, want true")
	}
	if stats.FirstTasklistMsgRatio != 1.0/3.0 {
		t.Errorf("FirstTasklistMsgRatio = %v, want %v (1/3)", stats.FirstTasklistMsgRatio, 1.0/3.0)
	}
}

// TestTrajTaskList_MultipleLists: two valid lists → NLists=2, first at index 0.
func TestTrajTaskList_MultipleLists(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlList()), ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText(tlList()+" updated"), ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(5)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(6)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.NLists != 2 {
		t.Errorf("NLists = %d, want 2", stats.NLists)
	}
	if !stats.ListEverEstablished {
		t.Errorf("ListEverEstablished = false, want true")
	}
}

// TestTrajTaskList_ProseMentionNotCounted: a text block that merely QUOTES
// the marker in prose but has no END must not count as a valid list
// (and does count as malformed in this parser, matching the silent-drop class).
func TestTrajTaskList_ProseMentionNotCounted(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"timestamp":%q}`, jqText("quote "+tlBegin+" only, no end block"), ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":10,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(4)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	// A prose mention of the begin marker without an end counts as malformed
	// (the silent-drop class), not as a real list.
	if stats.NMalformed != 1 {
		t.Errorf("NMalformed = %d, want 1 (prose begin without end)", stats.NMalformed)
	}
	if stats.NLists != 0 || stats.ListEverEstablished {
		t.Errorf("prose mention counted as list: NLists=%d ever=%v, want 0/false",
			stats.NLists, stats.ListEverEstablished)
	}
}
