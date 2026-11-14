package main

// chat_seal_source_test.go — WHICH FACE a turn's line came from, and the
// reference the boundary renders for it.
//
// WHAT ONLY THIS FILE CAN SHOW. internal/lineedit proves the reference spelling
// and internal/inputsource proves the vocabulary; neither can show that the
// boundary is handed the right face. The decision is made from the READ ARM
// (`selfDriven`) and the notice peek, never from the text — and that is the
// property under test, because a line the model typed for itself and a line the
// operator typed are the same string.
//
// THE EMPTY ANSWER IS A CASE OF ITS OWN. A notice turn carrying a background
// job's completion is none of the three faces, and the boundary must render NO
// source field for it rather than guessing one (Rule 15a — an absent optional
// field stays absent).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/tools"
)

// noticeWithExternal builds the notice shape an inbox delivery produces: no job
// behind it, an External message carrying the face that wrote the record.
func noticeWithExternal(source string) tools.JobNotice {
	return tools.JobNotice{External: &tools.ExternalMessage{ID: "m-1", Message: "hello", Source: source}}
}

// jobCompletionNotice is the shape a background job's own notice has: no
// External message at all.
func jobCompletionNotice() tools.JobNotice {
	return tools.JobNotice{JobID: "j-1", Command: "sleep 1", Status: tools.JobExited, ExitCode: 0}
}

// TestChatSealSourceNamesTheArmNotTheText pins the FOUR answers and the empty
// one — one case per arm, so no arm can be folded into another (a `/queue`d
// prompt and a typed line are the same text and must be DIFFERENT faces).
func TestChatSealSourceNamesTheArmNotTheText(t *testing.T) {
	cases := []struct {
		name       string
		selfDriven bool
		queueTurn  bool
		notices    []tools.JobNotice
		want       string
	}{
		{"the model's own line", true, false, nil, inputsource.LLM},
		{"the operator's line", false, false, nil, inputsource.Human},
		{"a parked /queue prompt", false, true, nil, inputsource.Queue},
		{
			// The queue arm wins over the notice arm: they are not both true in
			// the loop (a queued line is taken only when nothing else is in
			// hand), but the precedence is stated so a fifth arm is a decision.
			"a parked prompt on a turn carrying deliveries",
			false,
			true,
			[]tools.JobNotice{noticeWithExternal(inputsource.Notify)},
			inputsource.Queue,
		},
		{
			// The model's arm is FIRST and stays first: a line the model typed
			// is the LLM's whatever else an iteration carried.
			"the model's line on a queued arm",
			true,
			true,
			nil,
			inputsource.LLM,
		},
		{"a notify delivery", false, false, []tools.JobNotice{noticeWithExternal(inputsource.Notify)}, inputsource.Notify},
		{"a delivery with no stated face", false, false, []tools.JobNotice{noticeWithExternal("")}, ""},
		{"a job completion", false, false, []tools.JobNotice{jobCompletionNotice()}, ""},
		{
			// The model's own arm wins: a line the model typed is the LLM's
			// whatever else is pending, because the notice is what the turn
			// OPENED from, not what it carries.
			"the model's line on a notice turn",
			true,
			false,
			[]tools.JobNotice{noticeWithExternal(inputsource.Notify)},
			inputsource.LLM,
		},
		{
			// A turn carrying deliveries from TWO faces is none of the four
			// spellings: the boundary renders no source field rather than a
			// composite this vocabulary would have to invent. The payload still
			// carries a face a reader can name (each inbox record has its own),
			// and the reference is not the place to reconcile them.
			"two deliveries from different faces",
			false,
			false,
			[]tools.JobNotice{
				noticeWithExternal(inputsource.Notify),
				noticeWithExternal(inputsource.Notify),
				noticeWithExternal(inputsource.Human),
			},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatSealSource("", tc.selfDriven, tc.queueTurn, tc.notices); got != tc.want {
				t.Errorf("chatSealSource(selfDriven=%v, queueTurn=%v, %d notices) = %q, want %q",
					tc.selfDriven, tc.queueTurn, len(tc.notices), got, tc.want)
			}
			if tc.want != "" && !inputsource.Known(tc.want) {
				t.Errorf("the fixture's own expectation %q is not a face", tc.want)
			}
		})
	}
}

// TestChatSealSourceArmsAreDistinguishable is the OTHER half of the same
// property, asserted as a SET rather than case by case: the four arms must yield
// four DIFFERENT values. A table can drift into passing while two arms return the
// same string (that is the defect this whole task is about — `queue` returning
// `human`); this fails on the day it happens, whatever the table says.
func TestChatSealSourceArmsAreDistinguishable(t *testing.T) {
	arms := []struct {
		name string
		got  string
	}{
		{"selfDriven", chatSealSource("", true, false, nil)},
		{"queueTurn", chatSealSource("", false, true, nil)},
		{"notify", chatSealSource("", false, false, []tools.JobNotice{noticeWithExternal(inputsource.Notify)})},
		{"human", chatSealSource("", false, false, nil)},
	}
	byValue := map[string]string{}
	for _, a := range arms {
		if a.got == "" {
			t.Errorf("the %s arm names no face — an arm that renders no source field is not an arm", a.name)
			continue
		}
		if prev, dup := byValue[a.got]; dup {
			t.Errorf("the %s arm and the %s arm both answer %q — two arms, one spelling", a.name, prev, a.got)
		}
		byValue[a.got] = a.name
	}
	if len(byValue) != 4 {
		t.Errorf("the four arms yielded %d distinct faces, want 4 (%v)", len(byValue), byValue)
	}
}

// TestChatSealSpilledLineCarriesTheStatedFace is the rendering half: the
// reference the boundary puts on the operator's screen names the face it was
// told, and a face it was NOT told renders no source field.
func TestChatSealSpilledLineCarriesTheStatedFace(t *testing.T) {
	dir := t.TempDir()
	payload := strings.Repeat("p", lineedit.InputLineRuneBound+1)
	for _, source := range []string{inputsource.Notify, inputsource.LLM, inputsource.Queue, ""} {
		out, err := chatSealMessage(&chatSession{sessionDir: dir}, payload, true, source)
		if err != nil {
			t.Fatalf("chatSealMessage(source=%q): %v", source, err)
		}
		if !out.Spilled {
			t.Fatalf("an over-threshold line was not spilled (source=%q)", source)
		}
		info, ok := lineedit.PasteRefInfoFor(out.Display)
		if !ok {
			t.Fatalf("the displayed line carries no reference: %q", out.Display)
		}
		if info.Source != source {
			t.Errorf("the reference names source %q, want %q (%q)", info.Source, source, out.Display)
		}
		if info.Size != len(payload) {
			t.Errorf("the reference names size %d, want %d", info.Size, len(payload))
		}
		if info.Path != out.PayloadPath {
			t.Errorf("the reference names %q, the payload is at %q", info.Path, out.PayloadPath)
		}
		if _, statErr := os.Stat(out.PayloadPath); statErr != nil {
			t.Errorf("the payload the reference names does not exist: %v", statErr)
		}
		if filepath.Dir(out.PayloadPath) != filepath.Join(dir, lineedit.PasteDirName) {
			t.Errorf("the payload landed in %s, want the session's own %s", filepath.Dir(out.PayloadPath), lineedit.PasteDirName)
		}
	}
}
