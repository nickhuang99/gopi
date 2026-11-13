package tools

// chat_policy_gate_test.go — the DECISION half of the chat write boundary: the
// four paths a command line can take (allow / prompt / forbidden / no rule at
// all), the compound-line rule, and the shapes the old verb/expansion special
// cases used to refuse.
//
// THE TABLES ARE ABOUT THE OPERATOR'S RULES, NOT ABOUT GIT. Each test freezes a
// rule table and a posture with SetChatPolicy, then asks the EXPORTED predicate
// what a chat turn would actually receive — so "the boundary allows X" is a
// statement about the tool call's result, never about a private helper.
//
// THE FOUR PATHS, and the ONE distinction that must never blur:
//
//	allow      -> "" (the command runs)
//	prompt     -> an APPROVAL REQUEST: not run, not refused, the model asks a human
//	forbidden  -> a REFUSAL: nothing to ask, the text names the rule
//	no rule    -> the POSTURE answers, and it answers in one of the three above
//
// The prompt/forbidden pair is the load-bearing assertion: collapsing either into
// the other is the failure this whole layer exists to prevent, so the tests key on
// the two distinguishable prefixes rather than on prose.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
)

// freezeChatRuleTable parses a ```chat-policy fence with the SAME parser the
// operator's file goes through and freezes it with posture. It registers the
// shipped defaults for cleanup so a test that forgets to restore cannot leak a
// widened boundary into the next one.
func freezeChatRuleTable(t *testing.T, fence string, posture chatpolicy.Posture) {
	t.Helper()
	pol, err := chatpolicy.Parse("test chat-policy", fence)
	if err != nil {
		t.Fatalf("the test's own rule table does not parse: %v", err)
	}
	SetChatPolicy(pol, posture)
	t.Cleanup(ResetChatPolicyToDefaults)
}

// chatPolicyFence wraps rule lines in the fence the parser recognises, so each
// test's table reads like the operator's own file.
func chatPolicyFence(lines ...string) string {
	return "```chat-policy\n" + strings.Join(lines, "\n") + "\n```\n"
}

// postureOnRequest is the shipped default posture (ask).
func postureOnRequest() chatpolicy.Posture {
	return chatpolicy.NewPosture(chatpolicy.DecisionPrompt, chatPolicyDefaultSource)
}

// postureNamed is the posture a `--policy VALUE` would resolve to.
//
// THE YOLO POSTURE IS DELIBERATELY NOT USED AS A NEUTRAL BACKGROUND in the
// rule-table tests: chatpolicy.Posture.Resolve short-circuits on an
// allow-everything posture and never consults the table (that is the package's
// stated contract — "yolo means every command runs, with no exception branch"), so
// a test that froze `yolo` and expected a rule to refuse would be asserting the
// opposite of the design. Those tests use on-request instead, and the yolo arm is
// asserted on its own in TestChatBashPolicyUnmatchedLineTakesThePosture.
func postureNamed(t *testing.T, word string) chatpolicy.Posture {
	t.Helper()
	p, ok := chatpolicy.PostureNamed(word, "--policy")
	if !ok {
		t.Fatalf("the posture word %q is not accepted — the alias table changed", word)
	}
	return p
}

// TestChatBashPolicyAllowPathIsASilentPass pins the ALLOW arm: the caller gets ""
// and the command runs.
func TestChatBashPolicyAllowPathIsASilentPass(t *testing.T) {
	freezeChatRuleTable(t, chatPolicyFence("allow *"), postureOnRequest())
	for _, cmd := range []string{
		"git status",
		"git log --oneline -5",
		"git add devdoc/x.md",
		"git commit -m wip",
		"git push --force origin master",
		"git reset --hard HEAD~1",
		"rm -rf build",
		"ssh -o StrictHostKeyChecking=no host git push",
		"ls",
	} {
		if got := ChatBashWriteRefusal(cmd); got != "" {
			t.Errorf("ChatBashWriteRefusal(%q) = %q, want \"\" under `allow *`", cmd, got)
		}
	}
}

// TestChatBashPolicyPromptPathIsAnApprovalRequestNotARefusal pins the PROMPT arm,
// which is the one a two-valued guard cannot express: the text must say the
// command did NOT run, must ask the model to consult the operator, and must NOT
// read as a refusal (a refusal would tell the model the act is wrong).
func TestChatBashPolicyPromptPathIsAnApprovalRequestNotARefusal(t *testing.T) {
	freezeChatRuleTable(t, chatPolicyFence("prompt *"), postureOnRequest())
	for _, cmd := range []string{"git push --force origin master", "git reset --hard HEAD~1", "ls"} {
		got := ChatBashWriteRefusal(cmd)
		if got == "" {
			t.Fatalf("ChatBashWriteRefusal(%q) returned \"\" under a `prompt *` table — prompt must never be an allow", cmd)
		}
		if !strings.HasPrefix(got, chatWriteApprovalPrefix) {
			t.Errorf("the prompt arm must open with the approval prefix %q, got:\n%s", chatWriteApprovalPrefix, got)
		}
		if strings.Contains(got, "Refused by the CHAT WRITE BOUNDARY") {
			t.Errorf("the prompt arm reads as a REFUSAL — the model would stop asking instead of asking:\n%s", got)
		}
		for _, want := range []string{"NOT run", "ask the operator", cmd} {
			if !strings.Contains(got, want) {
				t.Errorf("the approval text must contain %q, got:\n%s", want, got)
			}
		}
	}
}

// TestChatBashPolicyForbiddenPathIsARefusal pins the FORBIDDEN arm: a refusal that
// names the deciding rule (its line and source), carries no approval prefix, and
// hands the model the routes that remain.
func TestChatBashPolicyForbiddenPathIsARefusal(t *testing.T) {
	freezeChatRuleTable(t, chatPolicyFence("# the operator's own sentence", "forbidden *"), postureOnRequest())
	got := ChatBashWriteRefusal("git push --force origin master")
	if got == "" {
		t.Fatal("ChatBashWriteRefusal returned \"\" under a `forbidden *` table")
	}
	if strings.HasPrefix(got, chatWriteApprovalPrefix) {
		t.Errorf("the forbidden arm must NOT carry the approval prefix — there is nothing to ask:\n%s", got)
	}
	for _, want := range []string{
		chatBashRefusalMarker,
		"forbidden",
		"gopi task submit",
		"chat-policy",        // the operator's sentence is the way to widen it
		"test chat-policy:3", // the rule's own provenance: line 3 of the fence text
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal must contain %q, got:\n%s", want, got)
		}
	}
}

// TestChatBashPolicyUnmatchedLineTakesThePosture is the matched=false path: the
// table has NO opinion, so the posture answers — and each of the three postures
// answers differently for the same command.
func TestChatBashPolicyUnmatchedLineTakesThePosture(t *testing.T) {
	const cmd = "git push --force origin master"
	cases := []struct {
		posture string
		want    string // "", approval, refusal
	}{
		{"yolo", ""},
		{"on-request", "approval"},
		{"forbidden", "refusal"},
	}
	for _, tc := range cases {
		t.Run(tc.posture, func(t *testing.T) {
			// A table that matches nothing: its only rule names a command word
			// this line does not start with.
			freezeChatRuleTable(t, chatPolicyFence("allow frobnicate"), postureNamed(t, tc.posture))
			got := ChatBashWriteRefusal(cmd)
			switch tc.want {
			case "":
				if got != "" {
					t.Fatalf("posture %q must allow an unmatched line, got:\n%s", tc.posture, got)
				}
			case "approval":
				if !strings.HasPrefix(got, chatWriteApprovalPrefix) {
					t.Fatalf("posture %q must ask for approval on an unmatched line, got:\n%s", tc.posture, got)
				}
			default:
				if got == "" || strings.HasPrefix(got, chatWriteApprovalPrefix) {
					t.Fatalf("posture %q must REFUSE an unmatched line, got:\n%s", tc.posture, got)
				}
			}
		})
	}
}

// TestChatBashPolicyCompoundLineTakesTheMostRestrictive pins the clause rule: a
// line is not judged by the clause the operator happened to write first. Each
// case pairs an allowed clause with a forbidden one, in both orders.
func TestChatBashPolicyCompoundLineTakesTheMostRestrictive(t *testing.T) {
	freezeChatRuleTable(t, chatPolicyFence("allow git status", "forbidden git push"), postureOnRequest())
	for _, cmd := range []string{
		"git status; git push --force",
		"git push --force && git status",
		"git status && git push --force origin master",
		"git status\n git push",
		"git status | git push",
	} {
		got := ChatBashWriteRefusal(cmd)
		if got == "" {
			t.Errorf("ChatBashWriteRefusal(%q) allowed a compound line holding a forbidden clause", cmd)
			continue
		}
		if !strings.Contains(got, "forbidden") {
			t.Errorf("the refusal must be the FORBIDDEN one (the strictest clause), got:\n%s", got)
		}
	}
	// The control: two allowed clauses stay a silent pass, so the test above is
	// not passing because the guard refuses every compound line.
	if got := ChatBashWriteRefusal("git status; git status"); got != "" {
		t.Errorf("a line of two allowed clauses was refused:\n%s", got)
	}
}

// TestChatBashPolicyDecidesShapesTheOldReaderRefusedByItself pins the DELETION
// this rewrite is for: a shell expansion, a home-relative operand, a glob, a
// redirection and an ssh invocation carrying options are WORDS to the rule table
// now — not "unreadable, therefore refused". Each is allowed under `allow *` and
// refused under `forbidden *`, i.e. the answer comes from the rules alone.
func TestChatBashPolicyDecidesShapesTheOldReaderRefusedByItself(t *testing.T) {
	shapes := []string{
		"git add $FILES",
		"git add ~/notes.md",
		"git add devdoc/*.md",
		"git add devdoc/x.md >/tmp/log 2>&1",
		"git -C $REPO commit -m x",
		"ssh -o StrictHostKeyChecking=no -p 2222 host git push",
		"bash -c 'git push --force'",
		"eval \"$(cat script.sh)\"",
	}
	freezeChatRuleTable(t, chatPolicyFence("allow *"), postureOnRequest())
	for _, cmd := range shapes {
		if got := ChatBashWriteRefusal(cmd); got != "" {
			t.Errorf("ChatBashWriteRefusal(%q) refused an ALLOWED line — a shape special case survived:\n%s", cmd, got)
		}
	}
	freezeChatRuleTable(t, chatPolicyFence("forbidden *"), postureOnRequest())
	for _, cmd := range shapes {
		got := ChatBashWriteRefusal(cmd)
		if got == "" {
			t.Errorf("ChatBashWriteRefusal(%q) allowed a FORBIDDEN line", cmd)
		}
	}
}

// TestChatPolicyFrozenDefaultsAreOnRequest pins the SHIPPED pair: the process
// starts on the built-in table and the asking posture, so a run that resolved no
// channel asks rather than runs.
func TestChatPolicyFrozenDefaultsAreOnRequest(t *testing.T) {
	ResetChatPolicyToDefaults()
	pol, posture := ChatPolicyFrozen()
	if pol == nil || pol.Len() == 0 {
		t.Fatal("the default rule table is empty — the guard would judge by nothing at all")
	}
	if got := posture.Decision(); got != chatpolicy.DecisionPrompt {
		t.Errorf("the default posture is %q, want %q — an unresolved run must ASK", got, chatpolicy.DecisionPrompt)
	}
	if got := ChatBashWriteRefusal("git push --force origin master"); !strings.HasPrefix(got, chatWriteApprovalPrefix) {
		t.Errorf("with no rules resolved, a command must be a question for the operator, got:\n%s", got)
	}
	if got := ChatBashWriteRefusal(""); got != "" {
		t.Errorf("a blank command line carries no judgement, got:\n%s", got)
	}
}

// TestChatBashPolicyGatesTheCleanupFaceApplyEvenWhenAllowed pins the ONE narrowing
// kept beside the rules: the cleanup faces' destructive half is the operator's own
// turn, and it is asked LAST — so `allow *` does not open it, while the operator's
// own words do.
func TestChatBashPolicyGatesTheCleanupFaceApplyEvenWhenAllowed(t *testing.T) {
	freezeChatRuleTable(t, chatPolicyFence("allow *"), postureOnRequest())
	apply := cleanupFaceApply()
	if got := ChatBashWriteRefusalAuthorized(apply, "", ""); got == "" {
		t.Fatal("`allow *` opened the cleanup face's destructive half with no operator turn")
	}
	if got := ChatBashWriteRefusalAuthorized(apply, "", "please clean up the finished runs"); got != "" {
		t.Fatalf("the operator's own authorizing turn was ignored:\n%s", got)
	}
	if got := ChatBashWriteRefusalAuthorized(cleanupFacePlan(), "", ""); got != "" {
		t.Fatalf("the PLAN must always be reachable, got:\n%s", got)
	}
}
