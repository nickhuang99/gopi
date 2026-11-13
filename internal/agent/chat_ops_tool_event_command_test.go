package agent

// chat_ops_tool_event_command_test.go — the WIRE half of the long-command copy
// contract: ToolEvent.Command is the call's command, UNCUT, and it is chosen by
// the SAME parse that ToolCallSummary digests.
//
// The defect this answers (measured 2026-10-27): ToolEvent carried only Summary,
// which firstLineOf caps with a `…`, so a terminal renderer built on the event
// could never copy a >160-cell command. The fix adds a sibling field rather than
// widening Summary (the web transcript replays Summary's bytes), and the pair's
// correctness rests on two properties that are asserted here because nothing else
// can see them:
//
//  1. SUMMARY DID NOT MOVE. The digest of every argument shape is byte-identical
//     to what it was before the field existed — the fallback arm (no digestable
//     string argument) and the placeholder included. A change here is a wire
//     change for /api/transcript, which no renderer test would catch.
//  2. THE TWO HALVES DESCRIBE THE SAME ARGUMENT. When Command is non-empty it is
//     the very argument Summary was read from — not a different key, and not a
//     re-parse down a list that happened to land elsewhere. A `read` call (path,
//     no command) therefore carries NO Command, so its row still renders the path
//     digest exactly as before.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// toolEventTestLongCommand is a command wider than the digest cap, with the tail
// token a truncation would eat.
const toolEventTestLongCommand = "bash -lc 'for f in $(git ls-files \"*.go\"); do grep -n TODO \"$f\"; done | head -200 && echo TOOL_EVENT_TAIL && git log --oneline --all --decorate --graph -40 -- internal/tools/task_finish_cleanup.go && echo TAIL2'"

// TestToolEventCommandIsTheUncutCommand pins the call event's two halves.
func TestToolEventCommandIsTheUncutCommand(t *testing.T) {
	tc := deepseek.ToolCall{Name: "bash", Arguments: map[string]interface{}{"command": toolEventTestLongCommand}}
	ev := ToolCallEvent(tc)

	if ev.Kind != ToolEventKindCall || ev.Name != "bash" {
		t.Fatalf("event = %+v, want a call event named bash", ev)
	}
	if ev.Command != toolEventTestLongCommand {
		t.Fatalf("Command is not the call's command bytes:\n got %q\nwant %q", ev.Command, toolEventTestLongCommand)
	}
	if strings.Contains(ev.Command, digestEllipsis) {
		t.Fatalf("Command carries the digest cut glyph: %q", ev.Command)
	}
	// The premise the renderer's fallback is designed around: Summary IS cut, and
	// both halves therefore have to exist.
	if !strings.Contains(ev.Summary, digestEllipsis) {
		t.Fatalf("Summary = %q — the digest cap moved; this file's premise no longer holds", ev.Summary)
	}
	if !strings.HasPrefix(toolEventTestLongCommand, strings.TrimSuffix(ev.Summary, digestEllipsis)) {
		t.Fatalf("Summary is not a prefix of the command it digests:\n summary %q\n command %q", ev.Summary, toolEventTestLongCommand)
	}
}

// TestToolEventCommandOnlyForACommandArgument is the negative half: an argument
// that is a digest but NOT a command yields no Command, so the renderer falls back
// to Summary and every existing row stays byte-identical.
func TestToolEventCommandOnlyForACommandArgument(t *testing.T) {
	for _, tc := range []struct {
		name string
		call deepseek.ToolCall
		want string
	}{
		{"read-path", deepseek.ToolCall{Name: "read", Arguments: map[string]interface{}{"path": "/etc/hostname"}}, ""},
		{"grep-pattern", deepseek.ToolCall{Name: "keyword_search", Arguments: map[string]interface{}{"query": "ToolEvent"}}, ""},
		{"cmd-alias", deepseek.ToolCall{Name: "bash", Arguments: map[string]interface{}{"cmd": "ls -1"}}, "ls -1"},
		{"empty-command", deepseek.ToolCall{Name: "bash", Arguments: map[string]interface{}{"command": "", "path": "/tmp/x"}}, ""},
		{"no-arguments", deepseek.ToolCall{Name: "bash"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolCallEvent(tc.call).Command; got != tc.want {
				t.Fatalf("Command = %q, want %q", got, tc.want)
			}
			// Whatever the arm, the digest still speaks about the tool's own
			// argument — the field's presence never changes it.
			if got := ToolCallEvent(tc.call).Summary; got != ToolCallSummary(tc.call) {
				t.Fatalf("Summary = %q but ToolCallSummary says %q — the two must be one parse", got, ToolCallSummary(tc.call))
			}
		})
	}
}

// TestToolCallSummaryIsUnchangedByTheCommandField pins the wire bytes. Every case
// here is a value the web transcript may already have replayed; a change is a
// wire change and this test is what makes that a decision rather than an
// accident.
func TestToolCallSummaryIsUnchangedByTheCommandField(t *testing.T) {
	long := strings.Repeat("x", toolCallSummaryMaxChars+40)
	cases := []struct {
		name string
		call deepseek.ToolCall
		want string
	}{
		{"first-key-wins", deepseek.ToolCall{Arguments: map[string]interface{}{"path": "/a", "command": "ls"}}, "/a"},
		{"command-digest-is-capped", deepseek.ToolCall{Arguments: map[string]interface{}{"command": long}},
			long[:toolCallSummaryMaxChars] + digestEllipsis},
		{"no-arguments", deepseek.ToolCall{Name: "bash"}, "(no arguments)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolCallSummary(tc.call); got != tc.want {
				t.Fatalf("ToolCallSummary = %q, want %q", got, tc.want)
			}
		})
	}
	// The compact-JSON fallback keeps its own cap: a call with no digestable
	// string argument is rendered from its arguments, cut at the raw cap.
	fallback := ToolCallSummary(deepseek.ToolCall{Arguments: map[string]interface{}{
		"n": 1, "blob": strings.Repeat("y", toolCallSummaryRawMaxChars+50),
	}})
	if !strings.HasSuffix(fallback, digestEllipsis) {
		t.Fatalf("the raw-arguments fallback = %q, want it cut and marked", fallback)
	}
	if n := len(fallback); n > toolCallSummaryRawMaxChars+len(digestEllipsis) {
		t.Fatalf("the raw-arguments fallback is %d bytes, want at most %d", n, toolCallSummaryRawMaxChars+len(digestEllipsis))
	}
}
