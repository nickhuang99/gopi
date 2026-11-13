package agent

// chat_surface_fact_test.go — the REQUEST-side contract of the arm fact: the two
// chat assemblies carry the note when the run published one, carry nothing when
// it did not, and never carry a note for a capability this run does not have
// (a task run, or a chat with no tools on the wire).

import (
	"strings"
	"testing"
)

// factTestAgent builds the smallest agent both assemblies can render: the
// published fact plus a persona body, with no session logger (the block that
// would otherwise name a transcript is exercised by its own cases).
func factTestAgent(armed bool, path string) *Agent {
	a := &Agent{SystemPrompt: "base"}
	a.SetChatSurfaceFact(armed, path, true)
	return a
}

// TestChatSurfaceFactRidesBothChatAssemblies is the load-bearing half of the
// move: the fact reaches the REQUEST on the plain path and the tool-enabled one
// alike — one fact, two carriers, the same rendering.
func TestChatSurfaceFactRidesBothChatAssemblies(t *testing.T) {
	for _, tc := range []struct {
		tag    string
		prompt string
	}{
		{"plain path", assembleChatSystemPrompt(factTestAgent(true, "/x/chat-screen.dump"))},
		{"tools path", chatOpsSystemPrompt(factTestAgent(true, "/x/chat-screen.dump"))},
	} {
		if !strings.Contains(tc.prompt, ChatSurfaceFactMarker()) {
			t.Errorf("%s: the system prompt does not carry the arm fact at all", tc.tag)
		}
		if !strings.Contains(tc.prompt, "/x/chat-screen.dump") {
			t.Errorf("%s: the arm note does not name this run's artifact, which is the one operand the model cannot derive", tc.tag)
		}
		if !strings.Contains(tc.prompt, chatSurfaceFactArmedNote[:40]) {
			t.Errorf("%s: the note is not the ARMED wording", tc.tag)
		}
	}
}

// TestChatSurfaceFactStatesTheAbsenceInWords is the unarmed half, and it is the
// one that stops an absence from reading as a blank screen: the note has to say
// which absence it is (no observer), never imply the screen was empty.
func TestChatSurfaceFactStatesTheAbsenceInWords(t *testing.T) {
	prompt := chatOpsSystemPrompt(factTestAgent(false, ""))
	if !strings.Contains(prompt, chatSurfaceFactMarker) {
		t.Fatalf("an unarmed run's prompt carries no fact at all: the model would discover the absence by failing")
	}
	if !strings.Contains(prompt, "no observation") {
		t.Errorf("the unarmed note does not state the absence of OBSERVATION in words:\n%s", prompt)
	}
	if strings.Contains(prompt, "blank") && !strings.Contains(prompt, "never a blank screen") {
		t.Errorf("the unarmed note mentions a blank screen outside the sentence that denies it")
	}
}

// TestChatSurfaceFactIsSilentWhereNothingWasPublished is the negative half: a
// run that never published (every task run; a chat that installs no surface) gets
// no note, and a build with no tools on the wire gets none either — the note
// describes a TOOL, and an instruction the agent cannot act on is worse than none.
func TestChatSurfaceFactIsSilentWhereNothingWasPublished(t *testing.T) {
	if got := ChatSurfaceFact(&Agent{SystemPrompt: "base"}); got != "" {
		t.Errorf("an agent that published nothing renders %q, want the empty note", got)
	}
	if got := ChatSurfaceFact(nil); got != "" {
		t.Errorf("a nil agent renders %q, want the empty note", got)
	}
	// toolsOn=false is REFUSED at the publish seam, so the fact cannot leak onto a
	// wire that carries no chat_surface def.
	a := &Agent{SystemPrompt: "base"}
	a.SetChatSurfaceFact(true, "/x/dump", false)
	if got := ChatSurfaceFact(a); got != "" {
		t.Errorf("a publish with toolsOn=false left %q, want nothing", got)
	}
	// AND THE ASSEMBLIES AGREE: a task run's own prompt is the kernel's, and a
	// chat prompt with no fact carries no marker.
	if strings.Contains(assembleChatSystemPrompt(&Agent{SystemPrompt: "base"}), ChatSurfaceFactMarker()) {
		t.Errorf("the plain chat prompt carries the fact marker with nothing published")
	}
}
