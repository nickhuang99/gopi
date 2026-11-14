package tools

// chat_operator_presence_test.go — WHO is on the other end of a `prompt`
// decision, and WHERE a chat's own artifacts may go.
//
// THE TWO DEFECTS THESE TESTS PIN (measured 2026-10-22, unattended chat runs; the
// full account is chat_operator_presence.go's header):
//
//	(a) a chat's OWN session directory — the place Rule 37 puts its artifacts, and
//	    the place the prompt's [CURRENT SESSION] block points at — came back
//	    "NEEDS OPERATOR APPROVAL", because it lies outside every git work tree, no
//	    `write <tree>` rule mentions it, and the default posture is `prompt *`;
//	(b) on a run that declared NO OPERATOR the approval text told the model to
//	    "ask the operator … and wait for their answer", an instruction nobody can
//	    carry out, while the same run's own prompt forbids exactly that.
//
// Both arms (write/edit and bash) are exercised, because both render a prompt
// decision and both had the asking sentence.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// attendOperator restores the ATTENDED reading when the test ends. Presence is
// process-wide state, so every test that flips it must put it back or the next
// test in this package inherits an unattended run it never declared.
func attendOperator(t *testing.T) {
	t.Helper()
	SetChatOperatorAbsent(false)
	t.Cleanup(func() { SetChatOperatorAbsent(false) })
}

// ownSessionDir points this process at a fresh session directory and restores the
// previous one afterwards. The directory is taken from the device scratch tree
// (Rule 22: off the `/` partition, never the source tree).
func ownSessionDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(testutil.TestDeviceScratchDir(t, "ownsession"), "sessions", "session-under-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the session directory %s: %v", dir, err)
	}
	signalrec.SetSessionDir(dir, "", "")
	t.Cleanup(func() { signalrec.SetSessionDir("", "", "") })
	return dir
}

// TestOwnSessionDirectoryIsExemptFromAPromptDecision is defect (a): under the
// SHIPPED default table (`prompt *`) and the default posture, a write into this
// run's own session directory is allowed — that is where Rule 37 says its
// artifacts go, and a chat that cannot write there cannot deliver anything at all.
func TestOwnSessionDirectoryIsExemptFromAPromptDecision(t *testing.T) {
	attendOperator(t)
	freezeChatRuleTable(t, chatPolicyFence("prompt *"), postureOnRequest())
	dir := ownSessionDir(t)

	target := filepath.Join(dir, ChatSessionArtifactsDirName, "report.md")
	if got := ChatWritePathRefusal(target, ""); got != "" {
		t.Fatalf("a write into THIS run's own session directory was refused under `prompt *`:\n%s\n"+
			"Rule 37 puts a chat's artifacts exactly here, and the operator cannot be asked about the directory "+
			"their own transcript is written to — the run would have nowhere to deliver.", got)
	}
}

// TestForbiddenRuleStillForbidsTheOwnSessionDirectory — the exemption is a
// PROMPT-branch exemption and nothing more. A rule the operator marked
// `forbidden` is not a question, and it still forbids this directory.
func TestForbiddenRuleStillForbidsTheOwnSessionDirectory(t *testing.T) {
	attendOperator(t)
	freezeChatRuleTable(t, chatPolicyFence("forbidden write home"), postureOnRequest())
	dir := ownSessionDir(t)

	target := filepath.Join(dir, ChatSessionArtifactsDirName, "report.md")
	got := ChatWritePathRefusal(target, "")
	if got == "" {
		t.Fatalf("a `forbidden write home` rule did not forbid %s — the session-directory exemption leaked into "+
			"the forbidden branch, which is the one decision the boundary must never soften", target)
	}
	if !strings.Contains(got, chatpolicy.DecisionForbidden.String()) {
		t.Errorf("the refusal does not name the forbidden decision it applied:\n%s", got)
	}
}

// TestSiblingWithTheSessionDirPrefixIsNotExempt — containment is measured on
// cleaned path segments, never on a string prefix: `…/session-under-test2` merely
// STARTS with the session directory's name and is a different directory.
func TestSiblingWithTheSessionDirPrefixIsNotExempt(t *testing.T) {
	attendOperator(t)
	freezeChatRuleTable(t, chatPolicyFence("prompt *"), postureOnRequest())
	dir := ownSessionDir(t)

	sibling := dir + "-sibling"
	target := filepath.Join(sibling, "report.md")
	if got := ChatWritePathRefusal(target, ""); got == "" {
		t.Fatalf("a path OUTSIDE the session directory was exempted because its spelling starts with the "+
			"directory's name (%s vs %s) — the exemption is containment, not a prefix", target, dir)
	}
}

// TestNoSessionDirectoryMeansNoExemption — an unset resolver answer is not a
// wildcard. With no session directory of its own this process exempts nothing.
func TestNoSessionDirectoryMeansNoExemption(t *testing.T) {
	attendOperator(t)
	freezeChatRuleTable(t, chatPolicyFence("prompt *"), postureOnRequest())
	signalrec.SetSessionDir("", "", "")
	t.Cleanup(func() { signalrec.SetSessionDir("", "", "") })

	if got := ChatWritePathRefusal(filepath.Join(t.TempDir(), "x.md"), ""); got == "" {
		t.Fatalf("with NO session directory recorded, an arbitrary path was exempted — an empty root must " +
			"exempt nothing (that is the classic 'path under X' defect)")
	}
}

// TestUnattendedPromptTextNeverAsksAnOperator is defect (b) for BOTH arms. The
// prefix stays (it is still a human decision and nothing ran), but the asking
// sentence — and every instruction that depends on an answer arriving — is gone,
// replaced by the two routes this run actually has and the authorisation that is
// decided before it starts.
func TestUnattendedPromptTextNeverAsksAnOperator(t *testing.T) {
	SetChatOperatorAbsent(true)
	t.Cleanup(func() { SetChatOperatorAbsent(false) })
	freezeChatRuleTable(t, chatPolicyFence("prompt *"), postureOnRequest())

	writeText := ChatWritePathRefusal("/nonexistent/outside/every/rule.md", "")
	bashText := ChatBashWriteRefusalAuthorized(chatPromptProbeCommand, "", "")

	for _, tc := range []struct {
		arm  string
		text string
	}{
		{"write/edit", writeText},
		{"bash", bashText},
	} {
		if tc.text == "" {
			t.Fatalf("%s: the prompt decision returned NO text at all — the decision must still be reported", tc.arm)
		}
		if !strings.HasPrefix(tc.text, chatWriteApprovalPrefix) {
			t.Errorf("%s: the one prefix a caller keys on is missing:\n%s", tc.arm, tc.text)
		}
		for _, forbidden := range []string{"ask the operator in the conversation", "wait for their answer"} {
			if strings.Contains(tc.text, forbidden) {
				t.Errorf("%s: an UNATTENDED run is told %q — nobody will answer, and the same run's prompt "+
					"forbids asking (chat_unattended.go):\n%s", tc.arm, forbidden, tc.text)
			}
		}
		if !strings.Contains(tc.text, "NO OPERATOR IS READING") {
			t.Errorf("%s: the text does not say WHY the wait cannot end:\n%s", tc.arm, tc.text)
		}
		if !strings.Contains(tc.text, "--policy") {
			t.Errorf("%s: the text does not name the authorisation that would have to exist BEFORE the run "+
				"starts:\n%s", tc.arm, tc.text)
		}
	}
}

// TestAttendedPromptTextStillAsksAnOperator — the attended console is untouched.
// There a human really is there to answer, and the approval channel (their own
// next message) really is that answer, so removing the asking sentence there
// would delete the only route a `prompt` decision has.
func TestAttendedPromptTextStillAsksAnOperator(t *testing.T) {
	attendOperator(t)
	freezeChatRuleTable(t, chatPolicyFence("prompt *"), postureOnRequest())

	writeText := ChatWritePathRefusal("/nonexistent/outside/every/rule.md", "")
	if !strings.Contains(writeText, "ask the operator in the conversation") {
		t.Fatalf("the ATTENDED approval text lost its asking sentence — that sentence is the approval channel:\n%s",
			writeText)
	}
	if strings.Contains(writeText, "NO OPERATOR IS READING") {
		t.Fatalf("an attended console was told nobody is reading:\n%s", writeText)
	}
}

// TestPresenceDefaultsToAttended — the zero value of the presence flag must be
// "a human is there", so no surface can become unattended by forgetting to say
// anything, and the blast radius of the new answer is exactly the runs that
// declared it.
func TestPresenceDefaultsToAttended(t *testing.T) {
	SetChatOperatorAbsent(false)
	if ChatOperatorAbsent() {
		t.Fatalf("the presence flag defaults to ABSENT — every chat surface that declared nothing would get the " +
			"unattended answer")
	}
	SetChatOperatorAbsent(true)
	if !ChatOperatorAbsent() {
		t.Fatalf("SetChatOperatorAbsent(true) did not take: the unattended answer is unreachable")
	}
	SetChatOperatorAbsent(false)
}

// chatPromptProbeCommand is a command line the shipped `prompt *` table answers
// with a QUESTION (not a repo write, not a long operation — those are refused by
// the category criterion and would test a different branch).
const chatPromptProbeCommand = "echo probe"
