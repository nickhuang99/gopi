package agent

// chat_cleanup_authorization_test.go — the authorization channel AT THE DISPATCH,
// which is where it has to hold: chatToolWriteGuard receives the turn's own
// message from the agent, so the properties below are about the WIRING (does the
// agent publish its turn, and clear it) rather than about the word lists (those
// are pinned in internal/tools).
//
// THE PROPERTY THAT MATTERS MOST is the no-leak one, and it is why the channel is
// a turn-scoped field with a restore rather than a stored switch: an authorizing
// turn must not leave the NEXT turn authorized. A test that only checked "the
// authorized turn passes" would pass on a permanently-open gate too, so the pair
// is asserted together — including the state the agent is left holding after a
// turn returns, which is what the restore exists to clear.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// freezeAllowAllForTheCleanupGate freezes an allow-everything RULE TABLE for the
// duration of one test, at the dispatch's own seam (tools.SetChatPolicy is the ONE
// writer of the frozen pair).
//
// WHY IT IS A PRECONDITION RATHER THAN A CONVENIENCE: the cleanup gate is asked LAST,
// only for a line the rules already ALLOW (tools/chat_policy_gate.go) — so the shipped
// default table (`prompt *`) would answer every command here with an approval request
// and no test below would ever reach the gate it is about. Freezing `allow *` states
// the arrangement the gate exists for: the operator's rules permit the face, and the
// only thing left standing is THIS turn's own message.
func freezeAllowAllForTheCleanupGate(t *testing.T) {
	t.Helper()
	pol, err := chatpolicy.Parse("dispatch cleanup-gate test table", "```chat-policy\nallow *\n```\n")
	if err != nil {
		t.Fatalf("the test's own rule table does not parse: %v", err)
	}
	tools.SetChatPolicy(pol, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, "test"))
	t.Cleanup(tools.ResetChatPolicyToDefaults)
}

// cleanupApplyInvocation is the destructive spelling under test, composed from the
// guard's own face list (Rule 13/14) rather than typed.
func cleanupApplyInvocation() string {
	faces := tools.ChatCleanupFaceInvocations()
	if len(faces) == 0 {
		panic("the cleanup face list is empty — this suite would be vacuous")
	}
	return faces[0] + " " + tools.ChatTaskApplyFlag
}

// TestChatTurnRefusesCleanupApplyWithoutTheOperatorsWords is the closed half: a
// chat turn that runs the destructive half of a cleanup face is refused while its
// own message says nothing about cleanup.
func TestChatTurnRefusesCleanupApplyWithoutTheOperatorsWords(t *testing.T) {
	freezeAllowAllForTheCleanupGate(t)
	a := &Agent{ChatMode: true}
	defer a.publishChatTurn("what does the task list look like?")()
	out := a.executeToolDefault(chatBashCall(cleanupApplyInvocation()))
	if !strings.Contains(out, "CHAT WRITE BOUNDARY") {
		t.Fatalf("a chat turn ran the destructive cleanup half with no authorization in its own message, got:\n%s", out)
	}
	if !strings.Contains(out, tools.ChatTaskApplyFlag) {
		t.Errorf("Rule 15: the refusal must name the flag it refused, got:\n%s", out)
	}
	if !strings.Contains(out, "DO THIS INSTEAD") {
		t.Errorf("Rule 15: the refusal must carry the alternative (the bare plan), got:\n%s", out)
	}
}

// TestChatTurnRunsCleanupApplyWhenTheOperatorAsked is the open half: the SAME
// invocation passes once the turn's own message asks for cleanup. The gate is the
// message, so this is the only difference between the two cases.
func TestChatTurnRunsCleanupApplyWhenTheOperatorAsked(t *testing.T) {
	freezeAllowAllForTheCleanupGate(t)
	for _, turn := range []string{"please clean up the finished runs", "把这些完成的任务清理一下"} {
		a := &Agent{ChatMode: true}
		defer a.publishChatTurn(turn)()
		if out := a.executeToolDefault(chatBashCall(cleanupApplyInvocation())); strings.Contains(out, "CHAT WRITE BOUNDARY") {
			t.Fatalf("the operator's own turn (%q) did not open the gate:\n%s", turn, out)
		}
	}
}

// TestChatTurnLeavesNoAuthorizationBehind is the no-leak property at the wiring
// level, in the two shapes that would otherwise hide a stuck gate: the restored
// state after the turn returns, and a following turn that says nothing.
func TestChatTurnLeavesNoAuthorizationBehind(t *testing.T) {
	freezeAllowAllForTheCleanupGate(t)
	a := &Agent{ChatMode: true}

	// Turn 1: the operator authorizes, and the turn ends (the deferred restore
	// runs). The agent must not be left holding the authorization.
	func() {
		defer a.publishChatTurn("clean up the stale worktrees")()
	}()
	if a.chatTurnInput != "" {
		t.Fatalf("the agent kept the authorizing turn after it returned (%q) — the gate has state", a.chatTurnInput)
	}

	// Turn 2: it says nothing about cleanup, so the destructive half is refused.
	defer a.publishChatTurn("thanks, what is next?")()
	if out := a.executeToolDefault(chatBashCall(cleanupApplyInvocation())); !strings.Contains(out, "CHAT WRITE BOUNDARY") {
		t.Fatalf("an authorization leaked into a later turn — the destructive cleanup half ran unasked:\n%s", out)
	}
}

// TestChatTurnLeavesThePlanReachable pins the other half of the design at the
// dispatch: the PLAN is the safe half and must be runnable in every turn, however
// the operator's message reads. A gate that also blocked the plan would leave the
// console unable to report what it could reclaim.
func TestChatTurnLeavesThePlanReachable(t *testing.T) {
	freezeAllowAllForTheCleanupGate(t)
	for _, turn := range []string{"", "what is running?", "do NOT clean anything up"} {
		a := &Agent{ChatMode: true}
		defer a.publishChatTurn(turn)()
		for _, face := range tools.ChatCleanupFaceInvocations() {
			if out := a.executeToolDefault(chatBashCall(face)); strings.Contains(out, "CHAT WRITE BOUNDARY") {
				t.Fatalf("the PLAN (turn=%q, face=%q) was refused — the plan is always reachable:\n%s", turn, face, out)
			}
		}
	}
}

// TestWorkerTurnIsUnaffectedByTheCleanupGate is the red half at this seam: the
// channel belongs to the CHAT path, so a task run — whose work is exactly the
// branch and worktree lifecycle — must still run both halves.
func TestWorkerTurnIsUnaffectedByTheCleanupGate(t *testing.T) {
	freezeAllowAllForTheCleanupGate(t)
	a := &Agent{ChatMode: false}
	if out := a.executeToolDefault(chatBashCall(cleanupApplyInvocation())); strings.Contains(out, "CHAT WRITE BOUNDARY") {
		t.Fatalf("the WORKER path was gated by the chat boundary:\n%s", out)
	}
}

// chatBashCall builds the tool call a chat turn would emit.
func chatBashCall(command string) deepseek.ToolCall {
	return deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": command},
	}
}
