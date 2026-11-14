package agent

// chat_pa_duty_test.go — the PA operating-duty paragraph, asserted against the
// COMPOSED prompts (the strings the request builders send) rather than against a
// copy of the text:
//
//  1. BOTH chat paths carry it, each with the limb that is TRUE of that path —
//     the plain path (no tools on the wire) must never be told to run a check it
//     cannot run, and the tool-enabled path must be told the work is actionable
//     here;
//  2. the TASK prompt carries NEITHER (a worker's contract IS the committed
//     change; "the cheap work is yours" would be the opposite of its contract);
//  3. it sits AFTER the write boundary on both paths — the two are read together
//     as one order (what this surface may NOT do, then what it MUST do itself);
//  4. it is RENDERED, not typed: every name it spells (the task route, the peer
//     flag, the usage hint, the commit verb, the force vocabulary) comes from the
//     leaf constants, so a rename on the dispatch side cannot leave the prompt
//     teaching a route the CLI rejects;
//  5. it AGREES with the write boundary about what a task is (the routing
//     sentence the two paragraphs teach has one spelling), and it does NOT widen
//     the boundary — the enforcement half is chat_write_boundary_test.go's
//     TestChatPromptsCarryTheWriteBoundary, which this file must keep green.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/branchform"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestChatPADutyQuickOpsNeedNoPermission is clause (1b): the operator's ruling of
// 2026-10-09 — a memory operation and a light read-only investigation are the
// PA's own work, done without asking — is RENDERED into both chat prompts, and
// every band, argument name and channel in it is the value the GUARD applies
// (Rule 1/13/14 the same-origin half: a band change that forgot this paragraph
// fails HERE instead of teaching the model a band the guard does not apply).
func TestChatPADutyQuickOpsNeedNoPermission(t *testing.T) {
	// The clause must be non-vacuous before it is asserted on: its own marker,
	// the two operation families and the concession sentence.
	block := chatPADutyRules(true)
	for _, want := range []string{
		chatPADutyNoPermissionMarker,
		"NOT a permission question",
		"without asking anybody",
		toolnames.ToolMemory + "(action=" + toolnames.ToolMemoryActionSearch + ")",
		chatMemoryStoreAction(),
		toolnames.ToolRead,
		toolnames.ToolCodeSearch,
		toolnames.ToolKeywordSearch,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("clause (1b) does not name %q — the reader is not told this work is theirs to do:\n%s", want, block)
		}
	}

	// The CATEGORY criterion is the GUARD's own rendering, never typed: the class
	// that runs is stated in the very words the guard's refusal prints.
	for _, want := range []string{
		tools.ChatOpClassOperatorWork.String(),
		tools.ChatOpAutoEnv,
		tools.ChatOpAutoSettingsKey,
		tools.ChatOpChannelAuto,
		tools.ChatOpChannelAllAsk,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("clause (1b) does not render %q — the prompt and the guard would apply two different criteria:\n%s",
				want, block)
		}
	}

	// The two limbs: the tool-enabled path is told the work is actionable HERE;
	// the plain path is NOT told to run something it has no tool for.
	for name, prompt := range chatPADutyChatPrompts(t) {
		if !strings.Contains(prompt, chatPADutyNoPermissionMarker) {
			t.Errorf("%s: the chat prompt carries no (1b) clause:\n%s", name, prompt)
		}
	}
	if !strings.Contains(chatPADutyRules(true), chatPADutyQuickOpsTools) {
		t.Errorf("the tools limb does not tell the tools path the quick work is actionable here")
	}
	if !strings.Contains(chatPADutyRules(false), chatPADutyQuickOpsNoTools) {
		t.Errorf("the plain limb does not state what the no-tools path can do about it")
	}
	if strings.Contains(chatPADutyRules(false), chatPADutyQuickOpsTools) {
		t.Errorf("the plain path is told to run the work itself — a false instruction: no tool is on its wire")
	}

	// ONE CRITERION (the task's own constraint): the guard's verdict for the very
	// calls the clause names must be "admitted, no ask" — clause (1b) describes the
	// guard's decision, it does not invent a second one. The verdict is asked of
	// the CATEGORY predicate, with NOTHING declared and the default posture.
	for _, cmd := range []string{"ls internal/agent", "gopi session log --last", "git status"} {
		if adm := tools.ChatOpAdmit(cmd, ""); !adm.Allowed() {
			t.Errorf("clause (1b) says the quick read-only work needs no permission, but the guard refuses %q "+
				"with nothing declared (%s) — the two surfaces disagree about the same call", cmd, adm.Reason())
		}
	}
}

// TestChatPADutyQuickOpsKeepsTheOtherTwoClasses is the "does not move the line"
// half: naming the cheap work as permission-free must not be readable as "the PA
// needs no permission for anything". The TASK class and the OPERATOR-ONLY class
// are unchanged — asserted by their own cues, read out of the SAME rendered block
// the clause lives in.
func TestChatPADutyQuickOpsKeepsTheOtherTwoClasses(t *testing.T) {
	block := chatPADutyRules(true)
	for _, want := range []string{
		"ASKING IS STILL RIGHT",                       // (1b) keeps the ask for the LONG class
		"(2) A change to the REPOSITORY is YOURS TOO", // and no longer calls a repo write WRONG
		"OPERATOR-ONLY",                               // (3) unchanged
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the duty paragraph is missing %q — naming the cheap work as permission-free must not "+
				"widen the other two classes:\n%s", want, block)
		}
	}

	// The SCOPE of clause (1) is wide, and that width is the point of this task —
	// so the class it is bounded BY has to be as readable as the scope itself. The
	// one exclusion is named, (2b) states the single act it retires, and the
	// exclusion is bounded by CODING rather than by the SUBJECT MATTER (otherwise
	// "read this function" would be mistaken for the excluded work).
	for _, want := range []string{
		chatPADutyScope,
		"(2b) The ONE act the scope in (1) still retires",
		"not by the subject matter",
		"stops at a ref",
		// The bound must name BOTH halves or a reader can take the admitted
		// repository write (2) for a licence to move a ref: the act is the REF
		// MOVE, and what it keeps is the operator's hand for THIS turn.
		"the ref move stays OPERATOR-ONLY THIS TURN",
		"never the way a repository write becomes permissible",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the duty paragraph does not state %q — a wide scope with an unreadable bound is the "+
				"defect this clause closes:\n%s", want, block)
		}
	}
	if !strings.Contains(block, chatPADutyNoCodingDebug) {
		t.Errorf("the exclusion is not quoted from the operator's own ruling (%q)", chatPADutyNoCodingDebug)
	}

	// AND THE WIDTH IS NOT "no permission at all": the scope widens WHICH work is
	// the console's, never the ACTS. The operator's 2026-10-24 ruling REVERSED the
	// repository half — a chat that cannot commit cannot finish anything, and on
	// the night of the ruling the dispatched workers were the thing breaking the
	// machine, so "route it to a worker" routed the work to the unreliable party
	// (chat_ops_authorization.go's ChatOpAdmit states the reversal). What the
	// mechanism STILL keeps out is a long operation nobody authorised.
	if adm := tools.ChatOpAdmit("make test", ""); adm.Allowed() {
		t.Errorf("an unauthorised long operation is admissible with nothing declared (%s) — the widened scope "+
			"must not read as unlimited authority", adm.Reason())
	}
	// The repository half is now ADMITTED, at every posture, with no question —
	// and that is the assertion, so a future reversal cannot pass quietly.
	if adm := tools.ChatOpAdmit("sed -i s/a/b/ internal/tools/chat.go", ""); !adm.Allowed() {
		t.Errorf("a repository write is refused (%s) — the operator's 2026-10-24 ruling opened it: "+
			"a console that cannot commit cannot finish anything", adm.Reason())
	}

	// NO CLAUSE IS RENDERED TWICE (measured defect, 2026-10-09): clause (1b) was
	// spliced in by re-emitting the builder's (1) block with a copy of (2) pasted
	// on, so the model read the repository clause twice. A duplicated clause is not
	// cosmetic — the reader meets the same rule twice and the paragraph's own
	// numbering stops meaning "in this order". Asserting the COUNT (not the
	// sentence's presence, which both forms satisfy) is what distinguishes the two.
	for _, clause := range []string{
		"(1) YOU DO IT YOURSELF,",
		chatPADutyNoPermissionMarker,
		"(2) A change to the REPOSITORY is YOURS TOO",
		"(3) OPERATOR-ONLY",
	} {
		if n := strings.Count(block, clause); n != 1 {
			t.Errorf("clause %q is rendered %d times in the duty paragraph, want exactly once — a clause "+
				"repeated is a prompt the model reads twice:\n%s", clause, n, block)
		}
	}
}

// chatPADutyChatPrompts returns the two composed chat prompts, keyed the way a
// failure message should name them. Both are read through the composition points
// the request builders call, never through a helper they were told about.
func chatPADutyChatPrompts(t *testing.T) map[string]string {
	t.Helper()
	chatBoundaryFixtureRepo(t) // the block renders the repo it stands in
	return map[string]string{
		"plain (Agent.Chat)": chatSystemPromptServed(nil),
		"tools (Agent.ChatOps)": chatOpsSystemPrompt(
			&Agent{SystemPrompt: assembleSystemPrompt()}),
	}
}

// TestChatPADutyBothChatPathsCarryTheirOwnLimb is the prompt half: briefer says
// the duty reaches both carriers, each stating only what is true of it, and the
// task assembly states neither.
func TestChatPADutyBothChatPathsCarryTheirOwnLimb(t *testing.T) {
	plain := chatSystemPromptServed(nil)
	if !strings.Contains(plain, chatPADutyMarker) {
		t.Fatalf("the plain chat prompt carries no PA-duty paragraph:\n%s", plain)
	}
	if !strings.Contains(plain, chatPADutyNoToolsLimb) {
		t.Errorf("the plain chat prompt must carry the NO-TOOLS limb — no tool is on its wire")
	}
	if strings.Contains(plain, chatPADutyToolsLimb) {
		t.Errorf("the plain chat prompt carries the TOOLS limb — a false instruction: it can run nothing")
	}

	toolsPath := chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()})
	if !strings.Contains(toolsPath, chatPADutyMarker) {
		t.Fatalf("the --tools chat prompt carries no PA-duty paragraph:\n%s", toolsPath)
	}
	if !strings.Contains(toolsPath, chatPADutyToolsLimb) {
		t.Errorf("the --tools chat prompt must carry the TOOLS limb — bash/read/task_submit ARE on its wire")
	}
	if strings.Contains(toolsPath, chatPADutyNoToolsLimb) {
		t.Errorf("the --tools chat prompt carries the NO-TOOLS limb — a false instruction")
	}

	if task := assembleSystemPrompt(); strings.Contains(task, chatPADutyMarker) {
		t.Fatalf("the TASK prompt carries the PA-duty paragraph — a worker's deliverable IS the committed change:\n%s", task)
	}

	// The write boundary is NEVER widened by this paragraph: its marker and its
	// limbs are still there, on both paths, beside the new one.
	for _, path := range []struct{ name, prompt string }{
		{"plain", plain},
		{"tools", toolsPath},
	} {
		if !strings.Contains(path.prompt, chatWriteBoundaryMarker) {
			t.Errorf("%s: the write boundary is no longer in the prompt", path.name)
		}
	}
}

// TestChatPADutySitsAfterTheWriteBoundary pins the ORDER the two paragraphs are
// read in: the duty paragraph is LAST, so the block ends with the two orders —
// and the duty names the boundary as the thing it may not weaken.
func TestChatPADutySitsAfterTheWriteBoundary(t *testing.T) {
	for name, prompt := range chatPADutyChatPrompts(t) {
		boundary := strings.Index(prompt, chatWriteBoundaryMarker)
		duty := strings.Index(prompt, chatPADutyMarker)
		if boundary < 0 || duty < 0 {
			t.Fatalf("%s: both paragraphs must be present (boundary=%d duty=%d)", name, boundary, duty)
		}
		if duty < boundary {
			t.Errorf("%s: the duty paragraph renders BEFORE the write boundary (duty=%d boundary=%d) — the block must end with the orders, boundary first", name, duty, boundary)
		}
	}
	// And INSIDE the block, the duty's own sentence points at the boundary: the
	// two are one order, so a reader of (9) knows what (8) still forbids.
	block := chatContextRulesFor(true)
	for _, want := range []string{"write-boundary paragraph above", "already declares legal"} {
		if !strings.Contains(block, want) {
			t.Errorf("the duty paragraph does not cite the write boundary as the list it may not widen (%q missing):\n%s", want, block)
		}
	}
}

// TestChatPADutyNamesItsVocabularyFromTheLeafConstants is the Rule 1/13/14 half:
// every name the paragraph spells is the leaf constant's own value, so a rename
// on the dispatch side fails HERE instead of leaving the prompt routing the model
// into a refusal.
func TestChatPADutyNamesItsVocabularyFromTheLeafConstants(t *testing.T) {
	block := chatContextRulesFor(true)

	for _, c := range []struct{ name, value string }{
		{"tools.NotifyPeerFlag", tools.NotifyPeerFlag},
		{"cliverbs.Task", cliverbs.Task},
		// cliverbs.Notify replaced taskverbs.Sync here on 2026-11-06: the
		// cross-host face the paragraph teaches is `gopi notify --peer <alias>`
		// now, because `gopi task sync --peer` is a DELETED verb that answers
		// with a removal notice and exit 1. The assertion is unchanged in
		// KIND — the paragraph must render the leaf constant's own value —
		// and the constant is simply the live one.
		{"cliverbs.Notify", cliverbs.Notify},
		{"cliverbs.UsageHint", cliverbs.UsageHint},
	} {
		if strings.TrimSpace(c.value) == "" {
			t.Fatalf("%s is empty — the paragraph would render a bare word", c.name)
		}
		if !strings.Contains(block, c.value) {
			t.Errorf("the paragraph does not render %s (%q):\n%s", c.name, c.value, block)
		}
	}

	// The task route: ONE spelling, the same helper the write boundary renders —
	// so the two paragraphs cannot teach two different commands for one decision.
	route := chatTaskSubmitRoute()
	if !strings.Contains(block, route) {
		t.Errorf("the paragraph does not teach the route %q:\n%s", route, block)
	}
	if !strings.Contains(block, toolnames.ToolTaskSubmit) {
		t.Errorf("the paragraph does not name the tool the model actually calls (%s)", toolnames.ToolTaskSubmit)
	}

	// The one thing the PA may not do, read from the list the write boundary's
	// ENFORCEMENT reads (Rule 13/14): `add`/`commit` partly allowed, the commit
	// verb named here as forbidden to the console.
	// The word is PROSE now (the enforcement table it used to be read from was
	// deleted with the verb list), so the assertion is that it agrees with the ONE
	// other place that spells the same shape: the write boundary's allowance.
	// NOTE the direction: the commit is PERMITTED (2) — the operator's 2026-10-24
	// ruling — so this check keeps the two spellings of the word in step, and no
	// longer asserts that the paragraph forbids it.
	if got, want := chatCommitWord(), chatGitCommitVerbWord; got != want {
		t.Errorf("chatCommitWord() = %q, want %q (the write boundary's commit verb)", got, want)
	}
	if !strings.Contains(block, chatCommitWord()) {
		t.Errorf("the paragraph does not name the git verb %q (it must SAY the commit is yours, (2)):\n%s",
			chatCommitWord(), block)
	}

	// The force vocabulary is COMPOSED from internal/branchform, and the composed
	// form is what renders — each spelling appears (as an operand of the joined
	// list) and none of them is a literal in the source.
	for _, flag := range []string{
		branchform.FlagCreateForce, branchform.FlagSwitchCreateForce,
		branchform.FlagBranchForce, branchform.FlagBranchForceLong,
	} {
		if !strings.Contains(chatForceSpellings(), flag) {
			t.Errorf("the composed force list omits %q — a spelling the report must quote", flag)
		}
		if !strings.Contains(block, flag) {
			t.Errorf("the paragraph does not name the force spelling %q the operator alone may choose:\n%s", flag, block)
		}
	}

	// The operator's ruling is quoted, not paraphrased: clause (4) turns on the
	// exact phrase, so a paraphrase would be a different rule.
	if !strings.Contains(block, chatPADutyNoCodingDebug) {
		t.Errorf("the paragraph does not quote the ruling %q", chatPADutyNoCodingDebug)
	}
	// The three classes are all named, in the words a reader can act on.
	for _, want := range []string{"YOU DO IT YOURSELF", "(2) A change to the REPOSITORY is YOURS TOO", "OPERATOR-ONLY", "may READ", "may MEASURE", "VERIFY your own housekeeping"} {
		if !strings.Contains(block, want) {
			t.Errorf("the paragraph is missing the class/clause cue %q:\n%s", want, block)
		}
	}
}

// TestChatPADutyRendersNoDestructiveBranchForm is the gate-shaped half, and it
// is here rather than only in internal/standards because the prompt is a shipped
// INSTRUCTION: a force spelling taught beside a branch verb is a one-line ref
// destruction. The paragraph names the force vocabulary, so this asserts it names
// the FLAGS as a vocabulary and never renders a `git <verb> <flag>` form.
//
// The predicate is internal/branchform's (Rule 14 — the gate and this test read
// the same one), so a new destructive shape added there is caught here too.
func TestChatPADutyRendersNoDestructiveBranchForm(t *testing.T) {
	// Non-vacuity first: the predicate must actually recognise the shape it is
	// asked about, or this test would pass on any text at all.
	if got := branchform.Destructive("git checkout " + branchform.FlagCreateForce + " feat/x"); len(got) == 0 {
		t.Fatal("branchform.Destructive does not report a force-create form — this test would be vacuous")
	}
	for name, prompt := range chatPADutyChatPrompts(t) {
		for i, line := range strings.Split(prompt, "\n") {
			if forms := branchform.Destructive(line); len(forms) > 0 {
				t.Errorf("%s line %d renders a destructive branch-creation form %+v — a prompt is instruction:\n%s",
					name, i+1, forms[0], line)
			}
		}
	}
}

// TestChatPADutyAgreesWithTheWriteBoundaryOnWhatIsATask is the brief's
// consistency assertion: the routing decision "only a repository change is a
// task" is ONE sentence in the prompt, and both paragraphs reach it through the
// same composer — so the duty paragraph cannot route code to a worker while the
// boundary routes it somewhere else.
func TestChatPADutyAgreesWithTheWriteBoundaryOnWhatIsATask(t *testing.T) {
	route := chatTaskSubmitRoute()
	boundary, duty := chatWriteBoundaryRules(true), chatPADutyRules(true)
	for name, text := range map[string]string{"write boundary": boundary, "PA duty": duty} {
		if !strings.Contains(text, route) {
			t.Errorf("%s does not teach the route %q:\n%s", name, route, text)
		}
	}
	// The boundary states the sentence that routes a CODE change; the duty states
	// the test that classifies by CATEGORY, never by difficulty. Both must be
	// present, and they must not contradict: the duty routes repository changes
	// to the same route the boundary uses, and it claims no write of its own.
	if !strings.Contains(boundary, "CODE change is a task and not an edit") {
		t.Errorf("the write boundary no longer carries its routing sentence:\n%s", boundary)
	}
	if !strings.Contains(duty, "CATEGORY OF THE ACTION") || !strings.Contains(duty, "never its difficulty") {
		t.Errorf("the duty paragraph does not state the classification test:\n%s", duty)
	}
	// The duty paragraph widens nothing: it neither re-lists the banned verbs
	// (the boundary's job — two renderings drift) nor teaches a git mutation.
	if !dutyNamesNoBannedVerbList(duty) {
		t.Errorf("the duty paragraph re-renders the banned git verb list — that sentence belongs to the write boundary alone:\n%s", duty)
	}
}

// dutyNamesNoBannedVerbList reports whether the duty paragraph avoids re-rendering
// the write boundary's own sentence — that paragraph belongs to
// chatWriteBoundaryRules alone, and two renderings of one boundary is how they
// drift (Rule 13/14).
//
// IT CANNOT READ A VERB LIST ANY MORE: the compiled list the enforcement used to
// hold was deleted with the verb-shaped judgement (the bash boundary now decides
// from the operator's ```chat-policy fence), so there is no set to derive the
// check from. What remains a real property is that the duty paragraph does not
// restate the boundary's clause numbering or its marker.
func dutyNamesNoBannedVerbList(duty string) bool {
	return !strings.Contains(duty, chatWriteBoundaryMarker)
}
