package main

// cli_chat_dump_screen_self_serving_help_test.go — the `--dump-screen` help block
// is a description of an observation GOPI takes, not homework for the operator.
//
// THE DEFECT THIS PINS (measured, operator-reported 2026-09-27): the block opened
// with "THE RECIPE (in this order; each step is needed)" and a numbered list whose
// step 3 was `gopi ui_state --compare "<line>" --session <session-id> --target chat`.
// Read from the outside that is gopi asking a human to debug gopi's own screen: the
// operator has no line to name (the line is gopi's own output) and no reason to be
// told to "check a line you know". Both artifacts the comparison needs — the painted
// cell grid and the authoritative session transcript — belong to gopi, which already
// owns a verb that compares them and derives its own authority
// (`gopi ui_state --target chat --all`). So the help must SAY that, and this file
// holds the two ends of the claim:
//
//	GREEN — the block names the automatic audit, the agent that consumes it, and the
//	        exit contract that makes "could not run" distinguishable from "agreed";
//	RED   — the block carries no instruction addressed to a reader to run a typed
//	        "type / compare" step: no numbered recipe, no line to supply, no
//	        session id to pass.
//
// The GREEN half is not taken on trust either: the command the block names is
// PARSED through the verb's own parser and then RUN against a fixture, so a help
// text that drifted away from the argument vector the audit actually accepts fails
// here instead of in an operator's terminal (Rule 31: a surface in no execution has
// not been tested — and a help page is a surface).

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// chatSurfaceToolName is the tool the block must name as the reader the artifact
// exists for, read from the constants package that owns the spelling (Rule 1b:
// the name is compared on, so it is never typed).
const chatSurfaceToolName = toolnames.ToolChatSurface

// The vocabulary of the defect, named once (Rule 1: a string compared on is a
// constant, and every claim below reads the same spelling).
const (
	// chatDumpHelpRecipeMarker was the numbered-recipe heading — the shape that made
	// the block read as a task list handed to the operator.
	chatDumpHelpRecipeMarker = "THE RECIPE"
	// chatDumpHelpOperatorStepMarker was the step that asked the reader to produce
	// the line to check. A line is gopi's own output; nobody outside has one.
	chatDumpHelpOperatorStepMarker = "check a line you know"
	// chatDumpHelpTypedCompareMarker is the copy-pasteable form that made the check a
	// HUMAN's two-operand job (a line supplied by hand + a session id passed by hand).
	chatDumpHelpTypedCompareMarker = `--compare "<line>" --session`
	// chatDumpHelpSessionPlaceholder is the placeholder such a step carried.
	chatDumpHelpSessionPlaceholder = "--session <session-id>"
)

// chatDumpHelpBlock returns the `--dump-screen` section of `gopi chat -h`, from its
// flag line to the next flag. Every claim here is made about THIS slice: the rest of
// the page legitimately says "type a line starting with '/'" (a slash command is the
// operator's own input), and a whole-page scan would fail on it.
//
// The section is a FAMILY now: the dump is ON BY DEFAULT, so the arm flag and its
// OFF switch (`--no-dump-screen`) describe one capability and belong to one block —
// the walk therefore steps over the off switch's own flag line instead of ending
// there, or the block would be cut to its first paragraph and every claim about the
// reader would be made about a page fragment.
func chatDumpHelpBlock(t *testing.T) string {
	t.Helper()
	start := strings.Index(chatUsage, "\n"+chatDumpFlag)
	if start < 0 {
		t.Fatalf("chatUsage has no %s section — the flag an operator arms a run with is undocumented", chatDumpFlag)
	}
	rest := chatUsage[start+1:]
	// The off switch's line belongs to this block; the NEXT flag that is not part
	// of the family ends it. Walking line by line (rather than slicing at the
	// first "\n--") is what keeps the family whole, and it stays honest for a
	// flag inserted before the off switch.
	offset := 0
	for _, line := range strings.Split(rest, "\n") {
		if offset > 0 && strings.HasPrefix(line, chatFlagPrefix) {
			if strings.HasPrefix(line, chatDumpFlag) || strings.HasPrefix(line, chatNoDumpFlag) {
				offset += len(line) + 1
				continue
			}
			return rest[:offset]
		}
		offset += len(line) + 1
	}
	t.Fatalf("the %s section runs to the end of chatUsage — it has no following flag to end at", chatDumpFlag)
	return ""
}

// TestChatDumpHelp_DescribesAnObservationTheAgentTakes is the RED direction:
// nothing in the block instructs the OPERATOR to run the check.
//
// The assertions are on the tell-tale shapes, not on the words: "compare" and
// "type" are legitimate inside a description of what the agent's own tool does
// (action=compare), so a word-ban would be both wrong and trivially bypassed.
// What is banned is the USER-ADDRESSED form — a numbered recipe, a line for the
// reader to know, and a hand-passed session id.
func TestChatDumpHelp_DescribesAnObservationTheAgentTakes(t *testing.T) {
	block := chatDumpHelpBlock(t)
	for _, banned := range []string{
		chatDumpHelpRecipeMarker,
		chatDumpHelpOperatorStepMarker,
		chatDumpHelpTypedCompareMarker,
		chatDumpHelpSessionPlaceholder,
	} {
		if strings.Contains(block, banned) {
			t.Errorf("the %s help still carries %q — that reads as gopi asking the operator to debug gopi's own screen:\n%s",
				chatDumpFlag, banned, block)
		}
	}
}

// TestChatDumpHelp_NamesTheAutomaticAudit is the GREEN direction: having removed the
// homework, the block must say what DOES consume the artifact — the agent from
// inside, and the verb that audits the whole answer in one call — plus the exit
// contract that keeps "the audit could not run" from reading as "the two sides
// agree". A page that merely deleted the recipe would leave the flag seeming to have
// no reader at all.
func TestChatDumpHelp_NamesTheAutomaticAudit(t *testing.T) {
	block := chatDumpHelpBlock(t)
	for _, want := range []string{
		// WHO: the agent, from inside its own turn.
		"gopi itself",
		chatSurfaceToolName,
		agent.ChatSurfaceActionSee,
		agent.ChatSurfaceActionCompare,
		// WHICH: the one call that audits the whole answer with nothing handed over.
		ChatAuditOneCallCommand,
		// WHY it needs nothing from the reader: the authority is derived from the dump.
		"derives its authority from the dump",
		// AND the contract: an unchecked screen must not read as a verified one.
		uiStateAuditWordCouldNotRun,
		uiStateAuditWordAgreed,
		uiStateAuditWordDisagreed,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the %s help does not name %q — the artifact's reader would be invisible on the page that creates it:\n%s",
				chatDumpFlag, want, block)
		}
	}
	// The flag itself stays documented: removing the homework must not remove the
	// arming, which is the thing that makes the observation exist at all.
	if !strings.Contains(block, "GOPI_CHAT_DUMP") {
		t.Errorf("the %s help no longer documents the environment spelling:\n%s", chatDumpFlag, block)
	}
}

// ChatAuditOneCallCommand is the automatic audit spelled as the help text spells it.
// It is assembled from the verb's own constants, so the assertion, the page and the
// parser cannot drift apart (Rule 13: a value typed in three places is three values).
var ChatAuditOneCallCommand = "gopi " + CliVerbUIState + " " + uiStateFlagTarget + " " +
	string(uiclient.SurfaceChat) + " " + uiStateFlagAll

// chatDumpHelpAuditArgv extracts the argument vector the help text names on the line
// carrying the automatic audit — the words after `gopi `, split on spaces, with the
// verb dropped. Extraction (rather than re-spelling the vector here) is the point:
// the assertion must be about the COMMAND ON THE PAGE.
func chatDumpHelpAuditArgv(t *testing.T) []string {
	t.Helper()
	for _, line := range strings.Split(chatDumpHelpBlock(t), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ChatAuditOneCallCommand) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(trimmed, "gopi "))
		return fields[1:] // drop the verb: parseUIStateArgs takes its own argv tail
	}
	t.Fatalf("the %s help names no line starting %q — the named command is not a command:\n%s",
		chatDumpFlag, ChatAuditOneCallCommand, chatDumpHelpBlock(t))
	return nil
}

// TestChatDumpHelp_TheNamedAuditCommandIsTheAutomaticOne pins that the command the
// help names is (a) what the verb's parser accepts, as the NO-CEREMONY invocation —
// no line, no session, whole-answer mode — and (b) what the verb RUNS: the same argv
// is executed against a real fixture and must produce a verdict with nothing handed
// over.
func TestChatDumpHelp_TheNamedAuditCommandIsTheAutomaticOne(t *testing.T) {
	argv := chatDumpHelpAuditArgv(t)
	req, help, err := parseUIStateArgs(argv)
	if err != nil {
		t.Fatalf("the help names %q, which the verb refuses: %v", argv, err)
	}
	if help {
		t.Fatalf("the help names %q, which the verb answers with its usage page", argv)
	}
	if !req.all {
		t.Errorf("the named command %q does not select the whole-answer audit (all=%t)", argv, req.all)
	}
	if req.target != uiclient.SurfaceChat {
		t.Errorf("the named command %q does not select the chat surface (target=%q)", argv, req.target)
	}
	if req.compare != "" || req.session != "" {
		t.Errorf("the named command %q hands something over (compare=%q session=%q) — the audit is supposed to derive its authority and need neither",
			argv, req.compare, req.session)
	}

	// (b) EXECUTED, not merely parsed: one armed chat dump + its own session record,
	// and the argv from the page must come back with a verdict.
	answer := "the audit compares this line without being handed anything"
	dump, _ := auditFixture(t, []string{answer}, []string{answer})
	// The fixture's dump is written into this process's profile; the verb discovers it
	// exactly as a real caller would (no --pid, no --session, no path).
	if !strings.Contains(dump, uiclient.ChatDumpFileName) {
		t.Fatalf("the fixture did not arm a %s artifact (got %q)", uiclient.ChatDumpFileName, dump)
	}
	out, code := uiStateRun(t, argv)
	if code != 0 {
		t.Fatalf("the command the %s help names exited %d against a real armed dump (want 0 = the audit RAN and the layers agreed):\n%s",
			chatDumpFlag, code, out)
	}
	for _, want := range []string{"outcome=" + uiStateAuditWordAgreed, "matched=1", "disagreed=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("the named audit's ledger does not report %q — the verdict is not the one the help promises:\n%s", want, out)
		}
	}
}

// TestChatDumpHelp_UnarmedIsNotAClaim is the other end of the same contract, and the
// reason the exit codes are named on the page: with NO dump armed the audit must
// COULD-NOT-RUN, never agreement. It is asserted here (not only in the verb's own
// tests) because the help block is what tells a reader how to read the code.
func TestChatDumpHelp_UnarmedIsNotAClaim(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	// An empty sessions tree: nothing armed, nothing painted.
	if err := os.MkdirAll(gopiDir+"/sessions", 0o755); err != nil {
		t.Fatalf("prepare the empty profile: %v", err)
	}
	out, code := uiStateRun(t, []string{uiStateFlagTarget, string(uiclient.SurfaceChat), uiStateFlagAll})
	if code == 0 {
		t.Fatalf("an audit with no dump armed exited 0 — the help's exit contract says 1 means the check COULD NOT RUN:\n%s", out)
	}
}
