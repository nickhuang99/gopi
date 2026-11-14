package tools

// chat_cleanup_authorization_test.go — the operator-authorization channel's
// three properties, each one a table.
//
// THE GATE (why it is not a flag, an env var or a setting): a chat MAY collect a
// finished run's ground, but only when the operator asked for cleanup in THIS
// turn's own message. The tests below pin that in both directions and — the
// load-bearing half — pin the NO-LEAK property: an authorizing turn followed by a
// turn that says nothing must be REFUSED, because the gate reads a function of one
// turn's text and holds no state a later turn could inherit (chat_cleanup_
// authorization.go states why a stored switch was rejected: Rule 35).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// freezeAllowAllForCleanupGate freezes an allow-everything RULE TABLE (posture
// on-request) for the duration of one test.
//
// IT IS THE PRECONDITION OF EVERY TEST IN THIS FILE, and it is spelled out rather
// than assumed: the cleanup gate is a NARROWING of an allow, asked LAST — a line the
// rules send to `prompt` or `forbidden` never reaches it (chat_policy_gate.go). So a
// test that left the shipped default table (`prompt *`) frozen would be measuring
// the POSTURE, not the gate. Freezing `allow *` here states the arrangement the gate
// is designed for: the operator's rules let the cleanup face run, and the ONE thing
// still standing between the console and a deletion is this turn's own message.
func freezeAllowAllForCleanupGate(t *testing.T) {
	t.Helper()
	pol, err := chatpolicy.Parse("cleanup-gate test table", chatPolicyFence("allow *"))
	if err != nil {
		t.Fatalf("the test's own rule table does not parse: %v", err)
	}
	SetChatPolicy(pol, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, chatPolicyDefaultSource))
	t.Cleanup(ResetChatPolicyToDefaults)
}

// cleanupFacePlan / cleanupFaceApply are the two invocations the tables below
// exercise, composed from the ONE list that defines the faces (Rule 13/14).
func cleanupFacePlan() string  { return chatCleanupFaces()[0].invocation }
func cleanupFaceApply() string { return cleanupFacePlan() + " " + ChatTaskApplyFlag }

// TestChatBashWriteRefusalGatesTheCleanupFacesDestructiveHalf is the channel's
// primary table: the PLAN is always reachable, the APPLY needs the operator's own
// words in that turn.
func TestChatBashWriteRefusalGatesTheCleanupFacesDestructiveHalf(t *testing.T) {
	freezeAllowAllForCleanupGate(t)
	cases := []struct {
		name         string
		command      string
		operatorTurn string
		wantRefusal  bool
	}{
		{"plan with no message", cleanupFacePlan(), "", false},
		{"plan, operator said nothing about cleanup", cleanupFacePlan(), "what is running today?", false},
		{"apply with no message", cleanupFaceApply(), "", true},
		{"apply, operator asked in English", cleanupFaceApply(), "please clean up the finished ones", false},
		{"apply, operator asked in Chinese", cleanupFaceApply(), "把这些完成的任务清理一下", false},
		{"apply, operator named the verb", cleanupFaceApply(), "run sweep-worktrees", false},
		{"apply, operator named the verb AND the flag", cleanupFaceApply(), "run sweep-worktrees --apply", false},
		{"apply, operator said reclaim", cleanupFaceApply(), "reclaim the stale grounds", false},
		{"apply, operator mentioned sweep", cleanupFaceApply(), "sweep the worktrees", false},
		{"apply, operator forbade it", cleanupFaceApply(), "do not clean up anything yet", true},
		{"apply, operator forbade it in Chinese", cleanupFaceApply(), "先不要清理", true},
		{"apply, operator asked an unrelated question", cleanupFaceApply(), "why did the worker stop?", true},
		{"apply, operator asked about a row's status", cleanupFaceApply(), "what does task 01a115 say?", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ChatBashWriteRefusalAuthorized(tc.command, "", tc.operatorTurn)
			if tc.wantRefusal {
				if got == "" {
					t.Fatalf("ChatBashWriteRefusalAuthorized(%q, turn=%q) allowed the destructive half with no authorizing word in the turn",
						tc.command, tc.operatorTurn)
				}
				if !strings.Contains(got, "CHAT WRITE BOUNDARY") {
					t.Errorf("the refusal must name the boundary, got:\n%s", got)
				}
				if !strings.Contains(got, ChatTaskApplyFlag) {
					t.Errorf("Rule 15: the refusal must name the flag it refused, got:\n%s", got)
				}
				if !strings.Contains(got, "DO THIS INSTEAD") {
					t.Errorf("Rule 15: the refusal must carry the alternative (the plan), got:\n%s", got)
				}
				return
			}
			if got != "" {
				t.Fatalf("ChatBashWriteRefusalAuthorized(%q, turn=%q) refused an authorized invocation:\n%s",
					tc.command, tc.operatorTurn, got)
			}
		})
	}
}

// TestChatBashWriteRefusalGatesEveryFace is the across-the-list check: the gate is
// a property of the FACES (one list), so a face added to chatCleanupFaces is
// gated in the same edit rather than being silently reachable.
func TestChatBashWriteRefusalGatesEveryFace(t *testing.T) {
	freezeAllowAllForCleanupGate(t)
	for _, face := range chatCleanupFaces() {
		t.Run(face.invocation, func(t *testing.T) {
			if got := ChatBashWriteRefusalAuthorized(face.invocation+" "+face.applyFlag, "", ""); got == "" {
				t.Fatalf("the destructive half of %q was allowed with no operator authorization", face.invocation)
			}
			if got := ChatBashWriteRefusalAuthorized(face.invocation+" "+face.applyFlag, "", "清理一下完成的任务"); got != "" {
				t.Fatalf("the destructive half of %q was refused although the operator asked for cleanup:\n%s",
					face.invocation, got)
			}
			if got := ChatBashWriteRefusalAuthorized(face.invocation, "", ""); got != "" {
				t.Fatalf("the PLAN of %q must always be reachable, got:\n%s", face.invocation, got)
			}
		})
	}
}

// TestChatBashWriteRefusalHoldsTheCleanupGateClosedByDefault pins the
// no-forgetting property: the spellings that do NOT carry a turn (the ones every
// other caller uses) keep the destructive half CLOSED, so the gate cannot be
// opened by a caller that merely omits an argument.
func TestChatBashWriteRefusalHoldsTheCleanupGateClosedByDefault(t *testing.T) {
	freezeAllowAllForCleanupGate(t)
	for _, tc := range []struct{ name, got string }{
		{"ChatBashWriteRefusal", ChatBashWriteRefusal(cleanupFaceApply())},
		{"ChatBashWriteRefusalIn", ChatBashWriteRefusalIn(cleanupFaceApply(), "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == "" {
				t.Fatalf("%s allowed the destructive half of a cleanup face — the gate must be closed for a caller with no turn to offer", tc.name)
			}
		})
	}
}

// TestChatTurnAuthorizesCleanupDoesNotLeakBetweenTurns is the property that makes
// this a channel rather than a switch: authorization is a pure function of the
// turn's text, so an authorizing turn does not make the NEXT one authorized.
func TestChatTurnAuthorizesCleanupDoesNotLeakBetweenTurns(t *testing.T) {
	freezeAllowAllForCleanupGate(t)
	authorizing := "please clean up the finished runs"
	if !ChatTurnAuthorizesCleanup(authorizing) {
		t.Fatalf("ChatTurnAuthorizesCleanup(%q) = false — the table above is vacuous", authorizing)
	}
	// Same channel, next turn: it says nothing about cleanup, so nothing is
	// authorized. There is no state to clear because none is held.
	if ChatTurnAuthorizesCleanup("thanks, what is next?") {
		t.Fatal("a turn that says nothing about cleanup authorized it — the gate has state")
	}
	if got := ChatBashWriteRefusalAuthorized(cleanupFaceApply(), "", "thanks, what is next?"); got == "" {
		t.Fatal("the destructive half was allowed on a turn that never mentioned cleanup")
	}
}

// TestChatTurnAuthorizesCleanupVocabulary pins the vocabulary's own edges, so a
// future edit to the word lists is a deliberate act rather than a silent widening
// of what opens the gate.
func TestChatTurnAuthorizesCleanupVocabulary(t *testing.T) {
	cases := []struct {
		turn string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"cleanup", true},
		{"Clean Up", true},
		{"clean-up please", true},
		{"let us sweep the stale grounds", true},
		{"回收掉没用的 worktree", true},
		{"git worktree remove /x", false},
		{"delete the worktree", false},
		{"do not clean up", false},
		{"never sweep anything", false},
		{"", false},
		{"run sweep-worktrees --apply for t-1", true},
		{"run sweep-worktrees for t-1", true},
		{"don't run sweep-worktrees --apply", true},
	}
	for _, tc := range cases {
		if got := ChatTurnAuthorizesCleanup(tc.turn); got != tc.want {
			t.Errorf("ChatTurnAuthorizesCleanup(%q) = %v, want %v", tc.turn, got, tc.want)
		}
	}
}

// TestChatCleanupApplyRequestsReadsOnlyTheFacesOwnClauses pins what the reader
// accepts as a request: the face's own clause with the apply flag, matched from
// the start of the clause — a command that merely MENTIONS the verb is not a
// request, and `--apply` spelled with a value is not the flag.
func TestChatCleanupApplyRequestsReadsOnlyTheFacesOwnClauses(t *testing.T) {
	cases := []struct {
		command string
		want    int
	}{
		{"gopi task sweep-worktrees --apply", 1},
		{"gopi task sweep-worktrees", 0},
		{"./bin/gopi task sweep-worktrees --apply --json", 1},
		{"gopi task sweep-worktrees --apply; echo done", 1},
		{"echo 'gopi task sweep-worktrees --apply'", 0},
		{"gopi task sweep-worktrees --apply=true", 0},
		{"gopi task show 01abc", 0},
	}
	for _, tc := range cases {
		if got := len(chatCleanupApplyRequests(tc.command)); got != tc.want {
			t.Errorf("chatCleanupApplyRequests(%q) found %d request(s), want %d", tc.command, got, tc.want)
		}
	}
}

// TestChatCleanupFacesRenderedMatchesTheTable is the Rule 13/14 check on the
// rendering source: what the prompt teaches (`ChatCleanupFacesRendered`) must be
// the invocations the gate guards, in the same order, with no extra spelling.
func TestChatCleanupFacesRenderedMatchesTheTable(t *testing.T) {
	rendered := chatCleanupFacesRendered()
	faces := chatCleanupFaces()
	if len(faces) == 0 {
		t.Fatal("chatCleanupFaces is empty — a prompt would teach no cleanup face")
	}
	for _, f := range faces {
		if !strings.Contains(rendered, "`"+f.invocation+"`") {
			t.Errorf("the rendered list %q omits the face %q", rendered, f.invocation)
		}
		if !strings.Contains(f.invocation, cliverbs.Task) {
			t.Errorf("the face %q must name the `%s` verb group", f.invocation, cliverbs.Task)
		}
	}
}
