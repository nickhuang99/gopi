package tools

// chat_approval_authorization_test.go — the MINIMAL REPRODUCTION of the deadlock
// the approval channel closes, plus the predicates that decide it.
//
// THE DEFECT, IN ONE SENTENCE: a `prompt` decision returned text telling the model
// to ask its operator and retry, and the retry met the same rule and the same
// text — so under the shipped `prompt *` table with the shipped on-request posture
// every bash and write call was dead forever, not questioned.
//
// So every arm below is driven in BOTH directions, and the RED direction is what
// makes the GREEN one evidence: "approved → runs" is meaningless if the same
// boundary runs everything anyway. The pairs are
//
//	prompt + no approval in this turn   -> NOT run (the approval text)
//	prompt + approval in this turn      -> runs ("" — the call proceeds)
//	forbidden + approval in this turn   -> still REFUSED (an approval is not a key)
//
// The third pair is the guard's own limit, and it is asserted here rather than
// left to the caller: the approval channel narrows to the prompt branch and must
// never reach a `forbidden` rule (Rule 37).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/devdoc"
)

// chatApprovalRepoRoot is the repository root the write arm resolves paths
// against — the same spelling the sibling tests use.
const chatApprovalRepoRoot = chatWriteTestRepoRoot

// chatApprovalProbeCommand is a command line that matches NO rule, so it takes the
// POSTURE's answer — which is exactly the shipped default situation (`prompt *`,
// on-request) and keeps the reproduction independent of any fence this test writes.
const chatApprovalProbeCommand = "echo hi"

// chatApprovalProbePath is a write target the shipped default table questions.
const chatApprovalProbePath = "internal/tools/probe-written-by-approval.go"

// chatApprovalYes / chatApprovalNo are the operator messages the axes turn on.
const (
	chatApprovalYes = "yes, go ahead"
	chatApprovalNo  = "no, do not run it"
)

// TestChatWriteBoundaryAPromptIsAnsweredByThisTurnsOperator is the MINIMAL
// REPRODUCTION, write/edit arm: under the SHIPPED defaults (built-in `prompt *`
// table + on-request posture) the same write target is (a) asked about and NOT
// written when this turn carries no approval, and (b) written when the operator's
// own message in this turn says yes.
//
// RED PROOF: before chat_approval_authorization.go landed, (b) returned the same
// approval text as (a) — the arm below fails with "the operator's approval did not
// release the write".
func TestChatWriteBoundaryAPromptIsAnsweredByThisTurnsOperator(t *testing.T) {
	resetChatPolicyForApprovalTest(t)

	// (a) NO approval in this turn: the call is questioned, not run.
	got := ChatWritePathRefusalAuthorized(chatApprovalProbePath, chatApprovalRepoRoot, "please write the probe file")
	if !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("a prompt decision with no approval in the turn did not return the approval text:\ngot %q", got)
	}

	// (b) THE OPERATOR SAID YES in this turn: the same call runs.
	if got := ChatWritePathRefusalAuthorized(chatApprovalProbePath, chatApprovalRepoRoot, chatApprovalYes); got != "" {
		t.Fatalf("the operator's approval did not release the write:\ngot %q", got)
	}

	// (c) A NEGATION IS NOT AN APPROVAL, even though it contains approval words
	// ("run it"): a message that says no must not authorize anything.
	if got := ChatWritePathRefusalAuthorized(chatApprovalProbePath, chatApprovalRepoRoot, chatApprovalNo); !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("a NEGATED message authorized the write:\ngot %q", got)
	}

	// (d) The two-argument spelling holds the channel CLOSED — the default guard
	// path cannot be opened by a caller that forgot to pass a turn.
	if got := ChatWritePathRefusal(chatApprovalProbePath, chatApprovalRepoRoot); !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("ChatWritePathRefusal (no turn) released the write:\ngot %q", got)
	}
}

// TestChatBashBoundaryAPromptIsAnsweredByThisTurnsOperator is the same reproduction
// on the bash arm — the other half of the boundary, so a fix on one arm cannot
// leave the other dead.
func TestChatBashBoundaryAPromptIsAnsweredByThisTurnsOperator(t *testing.T) {
	resetChatPolicyForApprovalTest(t)

	if got := ChatBashWriteRefusalAuthorized(chatApprovalProbeCommand, "", "run the probe please"); !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("a prompt decision with no approval in the turn did not return the approval text:\ngot %q", got)
	}
	if got := ChatBashWriteRefusalAuthorized(chatApprovalProbeCommand, "", chatApprovalYes); got != "" {
		t.Fatalf("the operator's approval did not release the bash command:\ngot %q", got)
	}
	if got := ChatBashWriteRefusal(chatApprovalProbeCommand); !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("ChatBashWriteRefusal (no turn) released the command:\ngot %q", got)
	}
}

// TestApprovalNeverOpensAForbiddenRule is the guard's limit, asserted: a rule the
// operator marked `forbidden` is not a question, so no affirmation in the operator's
// message may turn it into an allow (Rule 37 — the boundary's hard no-go does not
// move, and the approval channel is a narrowing of `prompt`, never a key).
func TestApprovalNeverOpensAForbiddenRule(t *testing.T) {
	freezeChatPolicyForApprovalTest(t, chatPolicyFence(
		"forbidden write internal *",
		"forbidden git push *",
	), postureOnRequest())

	// The write arm: forbidden under an enthusiastic approval.
	got := ChatWritePathRefusalAuthorized(chatApprovalProbePath, chatApprovalRepoRoot, "yes! approved, go ahead")
	if got == "" {
		t.Fatal("an approval released a FORBIDDEN write target — the approval channel reached past prompt")
	}
	if strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("a forbidden target answered as an approval question:\n%s", got)
	}
	// The bash arm, same rule.
	got = ChatBashWriteRefusalAuthorized("git push origin master", "", "yes, approved, go ahead")
	if got == "" {
		t.Fatal("an approval released a FORBIDDEN command")
	}
	if strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("a forbidden command answered as an approval question:\n%s", got)
	}
	// And under a wholly silent table the posture is asked instead — the control
	// that shows the two assertions above are about the RULE, not about a boundary
	// that refuses everything.
	freezeChatPolicyForApprovalTest(t, chatPolicyFence("allow write "+devdoc.Root+" *"), postureOnRequest())
	if got := ChatWritePathRefusalAuthorized(chatApprovalProbePath, chatApprovalRepoRoot, chatApprovalYes); got != "" {
		t.Fatalf("the non-forbidden control did not run under an approval:\ngot %q", got)
	}
}

// TestChatTurnApprovesTheToolQuestion pins the predicate's own vocabulary, because
// it is the ONE gate standing between "the model asked" and "a command ran". Each
// case is a message an operator really types; the pairs are the ones a substring
// matcher would get wrong.
func TestChatTurnApprovesTheToolQuestion(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"empty", "", false},
		{"plain yes", "yes", true},
		{"yes with sentence", "yes, go ahead and run it", true},
		{"ok", "ok", true},
		{"approved", "approved", true},
		{"go ahead", "go ahead", true},
		{"chinese yes", "可以", true},
		{"chinese go", "跑吧", true},
		{"chinese approved sentence", "同意，就这么做", true},
		{"negated english", "no, do not run it", false},
		{"negated chinese", "不要执行", false},
		{"never", "never do that", false},
		// THE BOUNDARY CASES: words that CONTAIN an approval word but are not one.
		{"token is not ok", "here is the token", false},
		{"look is not ok", "look at the file", false},
		{"broke is not ok", "it broke", false},
		{"unrelated question", "what does the task list look like?", false},
		// Punctuation and case are the operator's, not ours.
		{"upper", "YES", true},
		{"trailing punctuation", "sure.", true},
		{"parenthesised", "(ok)", true},
	}
	for _, tc := range cases {
		if got := ChatTurnApprovesTheToolQuestion(tc.msg); got != tc.want {
			t.Errorf("%s: ChatTurnApprovesTheToolQuestion(%q) = %v, want %v", tc.name, tc.msg, got, tc.want)
		}
	}
}

// resetChatPolicyForApprovalTest restores the SHIPPED pair and cleans up after the
// test: these arms are about the DEFAULT table, so the fixture must not inherit a
// table another test froze (the same discipline chat_policy_gate_test.go states).
func resetChatPolicyForApprovalTest(t *testing.T) {
	t.Helper()
	ResetChatPolicyToDefaults()
	t.Cleanup(ResetChatPolicyToDefaults)
}

// freezeChatPolicyForApprovalTest freezes one fence + posture for the test.
func freezeChatPolicyForApprovalTest(t *testing.T, fence string, posture chatpolicy.Posture) {
	t.Helper()
	pol, err := chatpolicy.Parse("approval-test", fence)
	if err != nil {
		t.Fatalf("parse the test fence: %v", err)
	}
	SetChatPolicy(pol, posture)
	t.Cleanup(ResetChatPolicyToDefaults)
}
