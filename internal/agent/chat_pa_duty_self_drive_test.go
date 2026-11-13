package agent

// chat_pa_duty_self_drive_test.go — clause (6) of the operating-duty paragraph:
// a LONG JOB IS CONTINUED BY THE SURFACE ITSELF, with `chat_surface action=type`,
// and never by asking the operator "shall I continue?".
//
// THE MEASURED DEFECT (operator instruction, 2026-10-25). A long job (verifying
// 17 pending task rows) ran out of one answer's length. The console closed the
// turn with "要我现在继续验剩下 8 条吗?" and then with "下一轮我跑" — and the
// operator named both errors at once: "你哪里有下一轮？…你能记得吗?" and then the
// cure it had already built: "你在任务结尾的时候自己呼叫一个toolcall把要下一轮的
// 请求使用surface什么的toolcall让输出变为输入就好像是用户给你提的请求，实际上是
// 你自己的请求，这样就转入下一轮了…而不是反反复复来请示用户."
//
// WHY A PROMPT RULE AND NOT A MECHANISM: the capability shipped long ago
// (`chat_surface action=type` enqueues the next turn; chat_surface.go's bound
// machinery is its guard). Nothing was missing from the TOOL — the surface did
// not know that this is how a long job is finished. The fix therefore lands on
// the prompt, and these tests pin the three properties that make the paragraph
// do its job: it RENDERS on the tools limb, it CARRIES the tool's own name (a
// rule that says "continue yourself" without naming the call is the same
// defect one level down), and it NAMES the two banned closings VERBATIM, because
// the model reproduces the sentence it was shown.
//
// The no-tools limb is pinned too, and for the reason the file's other limbs
// exist: that path has no tool on the wire, so "type your own next turn" would
// be a FALSE instruction — the worst kind, because it is followed.

import (
	"strings"
	"testing"
)

// selfDriveBothLimbs renders the duty paragraph for both limbs, the same shape
// chat_pa_duty_bug_discovery_test.go uses for clause (4b).
func selfDriveBothLimbs(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"tools limb":    chatPADutyRules(true),
		"no-tools limb": chatPADutyRules(false),
	}
}

// TestSelfDriveRuleRendersOnBothLimbs pins PRESENCE: the ruling is in the
// rendered paragraph whichever limb the surface is on. A clause that renders on
// only one limb is a rule half the consoles never read.
func TestSelfDriveRuleRendersOnBothLimbs(t *testing.T) {
	for limb, block := range selfDriveBothLimbs(t) {
		if !strings.Contains(block, chatPADutySelfDriveMarker) {
			t.Errorf("%s: the self-drive ruling is missing from the rendered duty paragraph.\n"+
				"The operator's standing instruction is that a long job is continued by typing the next turn "+
				"itself; a limb that does not carry it closes every unfinished job with a question.\n"+
				"wanted marker: %s", limb, chatPADutySelfDriveMarker)
		}
	}
}

// TestSelfDriveRuleNamesTheToolOnTheToolsLimb pins the sentence that makes the
// rule ACTIONABLE. "Continue yourself" without the call is the defect this
// clause exists to close, restated — the measured run knew the capability
// existed and still asked, so a paragraph that only says "do not ask" would
// leave the next run in exactly the same place.
func TestSelfDriveRuleNamesTheToolOnTheToolsLimb(t *testing.T) {
	block := chatPADutyRules(true)
	if !strings.Contains(block, chatPADutySelfDriveTool) {
		t.Fatalf("the tools limb must name the call verbatim (%q): a rule that says \"start the next turn "+
			"yourself\" without naming the tool is a rule the model cannot act on", chatPADutySelfDriveTool)
	}
	// The name must come from the tool registry, not be re-typed here.
	if !strings.Contains(chatPADutySelfDriveTool, "chat_surface") {
		t.Fatalf("chatPADutySelfDriveTool must carry the tool's registered name, got %q", chatPADutySelfDriveTool)
	}
	if !strings.Contains(chatPADutySelfDriveTool, "action=type") {
		t.Fatalf("chatPADutySelfDriveTool must carry the action verb, got %q", chatPADutySelfDriveTool)
	}
}

// TestSelfDriveRuleNamesTheTwoBannedClosings pins the NEGATIVE half verbatim.
// The measured failure was a specific sentence ("要我现在继续验剩下 8 条吗?" /
// "下一轮我跑"), so the paragraph names the two English shapes of it and says
// plainly that they END the job rather than pause it. Naming them is what makes
// the rule recognisable at the moment it would otherwise be obeyed.
func TestSelfDriveRuleNamesTheTwoBannedClosings(t *testing.T) {
	block := chatPADutyRules(true)
	for _, banned := range []string{"shall I continue", "next round I will"} {
		if !strings.Contains(block, banned) {
			t.Errorf("the paragraph must name the banned closing %q verbatim: a rule the model cannot "+
				"recognise in its own sentence is not enforced by anything", banned)
		}
	}
	if !strings.Contains(block, "there is no next round unless you typed it") {
		t.Errorf("the paragraph must state WHY the promise is fatal — that nothing runs after the turn ends")
	}
}

// TestSelfDriveRuleKeepsTheNoToolsLimbHonest pins the counterpart limb: a path
// with no tools cannot type its next turn, and telling it to would be an
// instruction it follows into a failure. It must instead say what remains.
func TestSelfDriveRuleKeepsTheNoToolsLimbHonest(t *testing.T) {
	block := chatPADutyRules(false)
	if strings.Contains(block, chatPADutySelfDriveTool) {
		t.Fatalf("the no-tools limb must NOT name %q as something to call: that path has no chat_surface "+
			"on the wire, so the name is an instruction it cannot carry out", chatPADutySelfDriveTool)
	}
	if !strings.Contains(block, "cannot type its own next turn") {
		t.Fatalf("the no-tools limb must say plainly that it cannot type its own next turn")
	}
	if !strings.Contains(block, "DONE") || !strings.Contains(block, "REMAINS") {
		t.Fatalf("the no-tools limb must ask for what is DONE and what REMAINS")
	}
}

// TestSelfDriveRuleIsRenderedExactlyOnce pins that the paragraph does not
// accumulate copies as clauses are added beside it: a duplicated ruling reads as
// emphasis and a reader can no longer tell which sentence is authoritative.
func TestSelfDriveRuleIsRenderedExactlyOnce(t *testing.T) {
	for limb, block := range selfDriveBothLimbs(t) {
		if got := strings.Count(block, chatPADutySelfDriveMarker); got != 1 {
			t.Errorf("%s: the self-drive ruling appears %d time(s), want exactly 1", limb, got)
		}
	}
}

// TestSelfDriveRuleLandsInsideTheDutyParagraph pins WHERE it renders: inside
// chatPADutyRules, after clause (5) (verify by result), so a reader meets it as
// the last instruction about what a turn's END must look like rather than
// floating after the limb that closes the paragraph.
func TestSelfDriveRuleLandsInsideTheDutyParagraph(t *testing.T) {
	block := chatPADutyRules(true)
	verifyAt := strings.Index(block, "(5) VERIFY your own housekeeping")
	selfDriveAt := strings.Index(block, chatPADutySelfDriveMarker)
	if verifyAt < 0 || selfDriveAt < 0 {
		t.Fatalf("both clauses must render (verify=%d, self-drive=%d)", verifyAt, selfDriveAt)
	}
	if selfDriveAt < verifyAt {
		t.Fatalf("the self-drive clause must follow clause (5), got it at %d before verify at %d",
			selfDriveAt, verifyAt)
	}
}
