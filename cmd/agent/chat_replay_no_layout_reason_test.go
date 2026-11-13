package main

// chat_replay_no_layout_reason_test.go — THE RED PROOF for case ② (epic
// 01a12af7): the replay refusal NAMES the cause it measured instead of printing
// a menu of three possibilities, and the cause comes from the ONE place that
// decides it.
//
// THE MEASURED FAILURE. `gopi chat --replay <session>` refused with
//   "this run has no interactive editor (piped input, --no-editor, or a terminal
//    too small for the chat layout)"
// on EVERY failure — a menu, not a diagnosis. The fourth cause (a terminal that
// answered but could not be opened for raw mode) was not in the menu at all, and
// a reader could not tell which of the three they had hit without going to the
// source. The playback controls are installed at the editor's key seam, so on
// that path announceStart() never runs and the key hint never appears — the fact
// a watcher standing at a hang needs most was the one the sentence omitted.
//
// WHAT THESE TESTS PIN.
//
//	① the reason the layer records is the reason the refusal prints — one value,
//	   carried from the deciding branch to the sentence (no second decision);
//	② the nil layer (a fixture, an early return) answers a SENTENCE, not a panic
//	   (Rule 19: the arm reporting a degradation must not be the thing that
//	   crashes);
//	③ a layer with no recorded cause says so rather than guessing one (Rule 15);
//	④ the four reasons are DISTINCT constants — a reason table that collapsed to
//	   one string would satisfy ① while telling the operator nothing. That is the
//	   assertion that makes this table worth having.

import (
	"strings"
	"testing"
)

func TestChatInputNoLayoutReasonReportsTheDecidedCause(t *testing.T) {
	for _, want := range []string{
		chatNoEditorReasonNoEditor,
		chatNoEditorReasonPipedStdin,
		chatNoEditorReasonTooSmall,
		chatNoEditorReasonTerminalRefused,
	} {
		in := &chatInput{noLayoutReason: want}
		if got := chatInputNoLayoutReason(in); got != want {
			t.Errorf("the layer's own reason was not reported: got %q, want %q", got, want)
		}
	}
}

func TestChatInputNoLayoutReasonIsTotalAndNeverGuesses(t *testing.T) {
	// ② a nil layer.
	if got := chatInputNoLayoutReason(nil); got != chatNoEditorReasonNoInputLayer {
		t.Errorf("a nil layer must be NAMED, not dereferenced: got %q", got)
	}
	// ③ a layer that recorded nothing. The zero value must state the gap: the
	// whole point of a measured reason is that it is not invented.
	if got := chatInputNoLayoutReason(&chatInput{}); got != chatNoEditorReasonUnrecorded {
		t.Errorf("an unrecorded cause must be stated, not invented: got %q", got)
	}
}

func TestChatNoEditorReasonsAreDistinct(t *testing.T) {
	// ④ The table is the point. Four causes that render one string would have
	// passed ① while leaving the operator exactly where they were: unable to
	// tell a pipe from a flag from a short terminal.
	seen := map[string]string{}
	for _, r := range []string{
		chatNoEditorReasonNoEditor,
		chatNoEditorReasonPipedStdin,
		chatNoEditorReasonTooSmall,
		chatNoEditorReasonTerminalRefused,
		chatNoEditorReasonNoInputLayer,
		chatNoEditorReasonUnrecorded,
	} {
		if other, dup := seen[r]; dup {
			t.Errorf("two causes render the same sentence (%q and %q): %q", other, r, r)
		}
		seen[r] = r
		if strings.TrimSpace(r) == "" {
			t.Error("a cause is the empty string — it would render as a blank reason")
		}
	}
	// --no-editor is the operator's own choice and is named by the FLAG
	// constant (Rule 1b): the sentence must carry the spelling the operator
	// typed, not a paraphrase of it.
	if !strings.Contains(chatNoEditorReasonNoEditor, chatFlagNoEditor) {
		t.Errorf("the --no-editor reason does not name the flag: %q", chatNoEditorReasonNoEditor)
	}
}

func TestChatReplayNoEditorNoticeCarriesTheCauseAndTheMissingControls(t *testing.T) {
	// The refusal SENTENCE itself: it must (a) take the cause as its argument,
	// (b) name what is missing — the playback controls, which live at the key
	// seam the layout owns — and (c) keep the reason a substitution, so the
	// render cannot silently drop it.
	if n := strings.Count(chatReplayNoEditorNotice, "%"); n != 1 {
		t.Fatalf("the notice takes %d verbs, want exactly 1 (the reason): %q", n, chatReplayNoEditorNotice)
	}
	if !strings.Contains(chatReplayNoEditorNotice, "%s") {
		t.Fatalf("the notice no longer renders the measured reason: %q", chatReplayNoEditorNotice)
	}
	rendered := strings.Replace(chatReplayNoEditorNotice, "%s", chatNoEditorReasonPipedStdin, 1)
	for _, want := range []string{
		chatReplayNotice,
		chatNoEditorReasonPipedStdin,
		"playback controls",
		"NOT installed",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the rendered refusal does not carry %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "or a terminal too small") {
		t.Errorf("the refusal is still a MENU of possibilities: %s", rendered)
	}
}
