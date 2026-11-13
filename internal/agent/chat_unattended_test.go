package agent

// chat_unattended_test.go — the unattended context's contract, pinned on the
// real surfaces the change touches: the two chat prompt assemblies, the
// chat_surface tool description, and the refusal that fires at the loop bound.
//
// WHY THE TEST DRIVES THE ASSEMBLIES AND NOT THE STRING. The whole feature is a
// prompt change (Rule 3: no new tool, no new mechanism), so the only thing worth
// pinning is what the WIRE receives: the value of the marker constant proves
// nothing if no assembly ever appends it, and a test that asserted the constant
// would keep passing after the block was dropped from both carriers.
//
// The three properties, each on its own axis: PRESENCE is conditional on the
// run's own declaration (both directions, both assemblies); the SIZE is bounded
// as the epic states it (≤3 lines); the WALL's remedy is branched (an unattended
// run is not handed an exit that requires a human who is not there — and the
// interactive sentence is UNCHANGED, which is the acceptance's "原文保留").

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// TestChatUnattended_ContextIsCarriedOnlyByADeclaredRun is the presence gate, in
// BOTH directions and on BOTH chat carriers: a run that did not declare itself
// unattended renders exactly the prompt it rendered before (an interactive
// console must not be told nobody is reading it — a lie, and one that would
// degrade the surface the operator uses daily), and a run that did carries the
// marker. Nil is included because both assemblies accept a nil agent and the
// task path's own registry does.
func TestChatUnattended_ContextIsCarriedOnlyByADeclaredRun(t *testing.T) {
	cases := []struct {
		name string
		ag   *Agent
		want bool
	}{
		{"nil agent", nil, false},
		{"a plain interactive run", &Agent{SystemPrompt: assembleSystemPrompt()}, false},
		{"a declared unattended run", &Agent{SystemPrompt: assembleSystemPrompt(), Unattended: true}, true},
	}
	for _, c := range cases {
		for _, carrier := range []struct {
			name string
			got  string
		}{
			{"plain chat (Agent.Chat)", assembleChatSystemPrompt(c.ag)},
			{"tools chat (Agent.ChatOps)", chatOpsSystemPrompt(c.ag)},
		} {
			has := strings.Contains(carrier.got, ChatUnattendedPromptHead())
			if has != c.want {
				t.Fatalf("%s / %s: marker present=%t, want %t\nprompt head: %q",
					c.name, carrier.name, has, c.want, head(carrier.got))
			}
		}
	}

	// The task path must NOT carry it at all: a dispatched task run is not this
	// context's business, and `assembleSystemPrompt` is the registry every
	// background run reads. This is the same shape the chat-context and
	// write-boundary blocks are pinned with.
	if task := assembleSystemPrompt(); strings.Contains(task, ChatUnattendedPromptHead()) {
		t.Fatalf("assembleSystemPrompt (the TASK registry) carries the unattended block — it belongs to the chat carriers alone")
	}
}

// TestChatUnattended_BlockIsAtMostThreeLines pins the size ceiling the epic
// states, in the unit the operator stated it in: RENDERED LINES. The block is
// paid for on every turn of an unattended run (and re-paid at every prefill,
// since an append-only history is resent each round), so its prose has a budget
// rather than an opinion — and a sentence added to it without a thought for the
// budget fails here.
func TestChatUnattended_BlockIsAtMostThreeLines(t *testing.T) {
	block := chatUnattendedContextRules()
	if lines := strings.Count(block, "\n") + 1; lines > chatUnattendedPromptLineCount {
		t.Fatalf("the unattended block renders %d lines, over its ceiling of %d:\n%s",
			lines, chatUnattendedPromptLineCount, block)
	}
	// And the SAME ceiling measured where it is paid: the assembled prompt may
	// not grow by more than the ceiling (the section separator's blank line is
	// part of the growth, so the two measurements agree — a two-line block would
	// already exceed three once joined).
	base := chatOpsSystemPrompt(&Agent{SystemPrompt: "base"})
	un := chatOpsSystemPrompt(&Agent{SystemPrompt: "base", Unattended: true})
	if grew := strings.Count(un, "\n") - strings.Count(base, "\n"); grew > chatUnattendedPromptLineCount {
		t.Fatalf("the unattended context grew the assembled prompt by %d lines, over the ceiling of %d",
			grew, chatUnattendedPromptLineCount)
	}
	// The three facts the measured A/B needed are all present: nobody answers,
	// the model is authorised to choose, and asking is named as the failure.
	for _, want := range []string{ChatUnattendedPromptHead(), "AUTHORISED", "FAILURE shape"} {
		if !strings.Contains(un, want) {
			t.Fatalf("the unattended context does not carry %q:\n%s", want, un)
		}
	}
}

// TestChatUnattended_WallRefusalBranchesOnWhoseRunThisIs is the SECOND half of
// the epic (the dead hole): when the self-driven loop is at its bound, the
// refusal's remedy must be one the reader can actually take. An interactive run
// keeps the historic sentence (a human really is there to type the next period,
// and the operator's acceptance requires the original text to survive); an
// unattended run is told to continue in its own answer and to choose for itself,
// and is NOT told to ask an operator.
//
// The bound itself is asserted UNCHANGED in the same test: this change redirects
// a refusal, it never relaxes the wall (no model self-renewal — the constant is
// still the ceiling, and exhaustion is still reached after exactly that many
// self-driven turns).
func TestChatUnattended_WallRefusalBranchesOnWhoseRunThisIs(t *testing.T) {
	// THE WALL IS PINNED BY ITS MECHANISM, NOT BY ITS OLD LITERAL.
	//
	// This assertion used to read `chatSurfaceLoopBound != 8`, and it went stale
	// the moment the operator raised the bound: chatSurfaceLoopBound is an ALIAS
	// of config.ChatSurfaceTurnsDefault (chat_surface.go — Rule 13/14: one
	// definition of the number, never a second copy), so the literal 8 stopped
	// describing anything and the whole package failed on every run.
	//
	// What is asserted instead is the three facts that make this a WALL rather
	// than a number, none of which is a re-typed literal:
	//
	//  1. the package's declared ceiling IS the config's default — the alias
	//     holds, so the refusal cannot fire at a bound the operator never
	//     declared, and a FUTURE silent raise of the package's own value (the
	//     class of edit that produced this bug) is caught here;
	//  2. it never exceeds the configurable CEILING — a bound beyond it is not a
	//     stronger guarantee, it is an unbounded loop wearing a number
	//     (config.ChatSurfaceTurnsCeiling's own reason);
	//  3. the floor sits BELOW it — exhaustion is reachable at all, which is the
	//     half a "raise the ceiling" edit would quietly destroy.
	//
	// The wall's own BEHAVIOUR is proved underneath, on the real tool: spendBound
	// drives a whole period and the refusal is what it returns. A relaxed bound
	// fails there too, because a loop that never refuses has no refusal to branch.
	//
	// The channels are cleared first: a stray GOPI_CHAT_SURFACE_TURNS in the
	// suite's environment would otherwise decide the ceiling, and then this
	// assertion would be measuring the environment instead of the mechanism
	// (the same pin the configured-bound cases use, Rule 32).
	pinTurnsChannels(t)
	if chatSurfaceLoopBound != config.ChatSurfaceTurnsDefault {
		t.Fatalf("chatSurfaceLoopBound = %d but config.ChatSurfaceTurnsDefault = %d — the bound must BE the config's default (one definition of the number, Rule 13/14); a package that carries its own copy of the number drifts from the operator's declaration",
			chatSurfaceLoopBound, config.ChatSurfaceTurnsDefault)
	}
	if chatSurfaceLoopBound > config.ChatSurfaceTurnsCeiling {
		t.Fatalf("chatSurfaceLoopBound = %d is past the configurable ceiling %d — a bound beyond it is not a stronger guarantee, it is an unbounded loop wearing a number",
			chatSurfaceLoopBound, config.ChatSurfaceTurnsCeiling)
	}
	if chatSurfaceLoopBound <= config.ChatSurfaceTurnsFloor {
		t.Fatalf("chatSurfaceLoopBound = %d does not clear the floor %d — the period would end before the first self-driven turn, so the refusal this test is about could never fire",
			chatSurfaceLoopBound, config.ChatSurfaceTurnsFloor)
	}
	// spendBound drives one agent's self-driven loop to its ceiling through the
	// REAL tool, so the refusal under test is the one a model actually receives.
	spendBound := func(t *testing.T, a *Agent) string {
		t.Helper()
		a.SetChatSurface(stubChatSurface{})
		period := a.ChatSurfaceLoopBound()
		for i := 0; i < period; i++ {
			if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "next"}); !strings.Contains(got, "typed=true") {
				t.Fatalf("type #%d returned %q, want an acceptance", i+1, got)
			}
			if turn := a.ChatSurfaceTakePending(); turn.Kind == ChatSurfaceSeamNone {
				t.Fatalf("consumption #%d handed out no turn", i+1)
			}
		}
		return a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "one more"})
	}

	interactive := spendBound(t, &Agent{})
	if !strings.Contains(interactive, "refused") || !strings.Contains(interactive, "what the operator should type") {
		t.Fatalf("the INTERACTIVE bound refusal lost its original remedy: %q", interactive)
	}

	unattended := spendBound(t, &Agent{Unattended: true})
	if !strings.Contains(unattended, "refused") || !strings.Contains(unattended, "bound") {
		t.Fatalf("the unattended bound refusal does not refuse by name: %q", unattended)
	}
	if strings.Contains(unattended, "what the operator should type") {
		t.Fatalf("the unattended bound refusal still sends the model to an operator who is not there: %q", unattended)
	}
	if !strings.Contains(unattended, "your answer IS the next step") {
		t.Fatalf("the unattended bound refusal does not name the exit that exists: %q", unattended)
	}
}

// TestChatUnattended_ToolDescriptionBranchesNotReplaces pins the tool-def side:
// the description is the tool's ONLY persuasion mechanism (Rule 11), so an
// unattended run reads the route that exists — while an interactive run's
// description is byte-identical to the one shipped before, which is what makes
// this a branch rather than a replacement.
func TestChatUnattended_ToolDescriptionBranchesNotReplaces(t *testing.T) {
	interactive := chatSurfaceToolDefFor(&Agent{})
	if interactive.Description != chatSurfaceToolDef().Description {
		t.Fatalf("the interactive tool description was modified — the autonomous route must be a BRANCH, not a replacement")
	}
	un := chatSurfaceToolDefFor(&Agent{Unattended: true})
	if un.Description == chatSurfaceToolDef().Description {
		t.Fatalf("the unattended run is sent the interactive description unchanged — it would read an exit that needs an operator")
	}
	if !strings.Contains(un.Description, "UNATTENDED") || !strings.Contains(un.Description, "do NOT ask the operator") {
		t.Fatalf("the unattended description does not redirect the route: %q", un.Description)
	}
	// The clone is deep enough for what changes: the NAME and the SCHEMA must be
	// the tool's, so a description branch can never fork the contract.
	if un.Name != chatSurfaceToolDef().Name {
		t.Fatalf("the unattended def renamed the tool: %q", un.Name)
	}
	if un.InputSchema == nil {
		t.Fatalf("the unattended def dropped the input schema — the action contract must survive the description branch")
	}
	if actions, ok := un.InputSchema.(map[string]interface{})["properties"]; !ok || actions == nil {
		t.Fatalf("the unattended def's schema lost its properties: %v", un.InputSchema)
	}
	if un.Name != toolnames.ToolChatSurface {
		t.Fatalf("def name %q, want %q", un.Name, toolnames.ToolChatSurface)
	}
}
