package agent

// chat_carrier_boundary_test.go — the CARRIER arm of the chat write boundary, at
// the one seam where it is enforced (Agent.executeToolDefault).
//
// THE DEFECT THIS PINS (measured, task 01a159ad). `gopi web --tools` drives the
// SAME engine the console drives, so its tool turns carry a.ChatMode=true and the
// chat write boundary judges them. With the shipped posture (`prompt *`) every
// write was answered
//
//	[NEEDS OPERATOR APPROVAL — command NOT run]
//
// and no file appeared — while the web page's own banner promises "turns run the
// full gopi toolset with no approval gate (loopback only, local-shell trust)". The
// question has no reader on that face: the person who typed the line IS the
// operator, and the model can only ask in conversation, which the HTTP turn
// renders back to them.
//
// THE THREE PROPERTIES, each its own arm below:
//
//	1. the CONSOLE is unchanged — `prompt` still means "ask the operator" (a
//	   human is in the turn);
//	2. a face that DECLARES no approval channel degrades `prompt` to the answer
//	   it declared before the turn started, so the tool really runs;
//	3. `forbidden` still wins on that face — the degradation is not a widening.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatCarrierWriteText is the payload the write arm deposits; a constant so the
// on-disk assertion and the call cannot drift.
const chatCarrierWriteText = "written by the carrier arm\n"

// chatCarrierApprovalMarker is the PREFIX that makes an approval request
// distinguishable from a refusal without parsing prose — taken from the tool
// layer's own rendering, so a reworded approval text keeps this test honest about
// which of the two answers it saw.
const chatCarrierApprovalMarker = "[NEEDS OPERATOR APPROVAL"

// chatCarrierRefusalMarker identifies a REFUSAL (nothing to ask), the answer a
// `forbidden` rule produces.
const chatCarrierRefusalMarker = "Refused by the CHAT WRITE BOUNDARY"

// TestChatCarrierWebRunsTheWriteTheConsoleAsksAbout is the RED-then-GREEN proof of
// the defect: the SAME call, on the same frozen pair, answered once for a console
// turn (a question, and no file) and once for a web turn (the write really lands).
func TestChatCarrierWebRunsTheWriteTheConsoleAsksAbout(t *testing.T) {
	tools.ResetChatPolicyToDefaults()
	t.Cleanup(tools.ResetChatPolicyToDefaults)

	target := filepath.Join(t.TempDir(), "made-by-the-web-face.txt")

	// (1) THE CONSOLE — unchanged behaviour: the shipped `prompt *` table makes the
	// write a question for the operator, and the file is NOT created. This is the
	// arm that proves the fix did not turn the boundary off everywhere.
	console := &Agent{ChatMode: true, ChatCarrier: tools.ChatPolicyCarrierTerminal}
	out := console.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolWrite,
		Arguments: map[string]interface{}{"path": target, "content": chatCarrierWriteText},
	})
	if !strings.Contains(out, chatCarrierApprovalMarker) {
		t.Fatalf("the CONSOLE turn's write was not a question for the operator, got:\n%s", out)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("the console's unanswered question still wrote %s", target)
	}

	// (2) THE WEB FACE — the same call, the same frozen pair, and the write runs:
	// the page declared "no approval gate" before the turn, so `prompt` has no
	// reader here and must not stop the turn.
	web := &Agent{ChatMode: true, ChatCarrier: tools.ChatPolicyCarrierWeb}
	out = web.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolWrite,
		Arguments: map[string]interface{}{"path": target, "content": chatCarrierWriteText},
	})
	if strings.Contains(out, chatCarrierApprovalMarker) || strings.Contains(out, chatCarrierRefusalMarker) {
		t.Fatalf("the WEB turn still got a boundary answer instead of running:\n%s", out)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the web face's write did not land at %s: %v (answer=%q)", target, err, out)
	}
	if string(got) != chatCarrierWriteText {
		t.Fatalf("the web face wrote %q, want %q", got, chatCarrierWriteText)
	}
}

// TestChatCarrierForbiddenStillWinsOnTheWebFace is the arm that keeps the
// degradation from becoming a widening: a rule the operator marked `forbidden`
// refuses on the web face exactly as it does on the console.
func TestChatCarrierForbiddenStillWinsOnTheWebFace(t *testing.T) {
	tools.ResetChatPolicyToDefaults()
	t.Cleanup(tools.ResetChatPolicyToDefaults)

	pol, err := chatpolicy.Parse("test chat-policy", "```chat-policy\nforbidden write *\n```\n")
	if err != nil {
		t.Fatalf("the test's own rule table does not parse: %v", err)
	}
	tools.SetChatPolicy(pol, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, "test"))
	t.Cleanup(tools.ResetChatPolicyToDefaults)

	for _, carrier := range []string{tools.ChatPolicyCarrierWeb, tools.ChatPolicyCarrierTerminal} {
		target := filepath.Join(t.TempDir(), "forbidden-on-"+carrier+".txt")
		a := &Agent{ChatMode: true, ChatCarrier: carrier}
		out := a.executeToolDefault(deepseek.ToolCall{
			Name:      toolnames.ToolWrite,
			Arguments: map[string]interface{}{"path": target, "content": chatCarrierWriteText},
		})
		if !strings.Contains(out, chatCarrierRefusalMarker) {
			t.Fatalf("carrier %q: a `forbidden write *` rule did not refuse, got:\n%s", carrier, out)
		}
		if strings.Contains(out, chatCarrierApprovalMarker) {
			t.Fatalf("carrier %q: a forbidden rule collapsed into an approval request:\n%s", carrier, out)
		}
		if _, err := os.Stat(target); err == nil {
			t.Fatalf("carrier %q: the refused write still created %s", carrier, target)
		}
	}
}

// TestChatCarrierBashArmUsesTheSameAnswer pins the SECOND arm: the bash tool is
// judged by the same carrier answer, so a face that may write may also run the
// command that writes. Proven by the filesystem (a directory that exists only if
// the command ran), never by the prose.
func TestChatCarrierBashArmUsesTheSameAnswer(t *testing.T) {
	tools.ResetChatPolicyToDefaults()
	t.Cleanup(tools.ResetChatPolicyToDefaults)

	dir := t.TempDir()

	consoleDir := filepath.Join(dir, "console-arm")
	console := &Agent{ChatMode: true, ChatCarrier: tools.ChatPolicyCarrierTerminal}
	out := console.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "mkdir -p " + consoleDir, "workdir": dir},
	})
	if !strings.Contains(out, chatCarrierApprovalMarker) {
		t.Fatalf("the console's bash call was not a question for the operator, got:\n%s", out)
	}
	if _, err := os.Stat(consoleDir); err == nil {
		t.Fatalf("the console's unanswered question still ran the command (%s exists)", consoleDir)
	}

	webDir := filepath.Join(dir, "web-arm")
	web := &Agent{ChatMode: true, ChatCarrier: tools.ChatPolicyCarrierWeb}
	out = web.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "mkdir -p " + webDir, "workdir": dir},
	})
	if _, err := os.Stat(webDir); err != nil {
		t.Fatalf("the web face's bash call did not run (%s missing): %v\nanswer:\n%s", webDir, err, out)
	}
}

// TestChatCarrierUnsetKeepsTheConsoleAnswer pins the SAFE DIRECTION: a turn that
// names no carrier (an old caller, a reused agent between turns) is treated as the
// console — the frozen posture answers, verbatim. Without this arm a zero value
// could become a silent widening.
func TestChatCarrierUnsetKeepsTheConsoleAnswer(t *testing.T) {
	tools.ResetChatPolicyToDefaults()
	t.Cleanup(tools.ResetChatPolicyToDefaults)

	target := filepath.Join(t.TempDir(), "no-carrier.txt")
	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolWrite,
		Arguments: map[string]interface{}{"path": target, "content": chatCarrierWriteText},
	})
	if !strings.Contains(out, chatCarrierApprovalMarker) {
		t.Fatalf("a turn naming NO carrier must keep the frozen posture's answer, got:\n%s", out)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("a turn naming no carrier still wrote %s", target)
	}
}

// TestChatOpsOnCarrierPublishesAndRestoresTheCarrier is the publication half: the
// carrier is set for the turn that declared it and RESTORED afterwards, so a
// reused agent cannot inherit one face's answer into another face's turn (the same
// property publishChatTurn has for the operator's authorization words).
func TestChatOpsOnCarrierPublishesAndRestoresTheCarrier(t *testing.T) {
	a := &Agent{}
	restore := a.publishChatCarrier(tools.ChatPolicyCarrierWeb)
	if a.ChatCarrier != tools.ChatPolicyCarrierWeb {
		t.Fatalf("the carrier was not published: %q", a.ChatCarrier)
	}
	restore()
	if a.ChatCarrier != "" {
		t.Fatalf("the carrier was not restored after the turn: %q", a.ChatCarrier)
	}
}
