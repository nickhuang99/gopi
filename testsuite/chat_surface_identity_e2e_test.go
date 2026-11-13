package testsuite

// chat_surface_identity_e2e_test.go — THE ACCEPTANCE RUN for this epic, on the
// real thing: a `gopi chat --tools` child, driving a REAL pty, whose `bash` tool
// runs `gopi ui_state --target chat`.
//
// WHAT IT PROVES, AND WHY NO UNIT TEST COULD. The claim under test is that a chat
// run's identity REACHES THE CHILD IT SPAWNS — through the environment `exec(2)`
// copies, not through anything this repository wrote down. Every piece of that
// sentence lives on the far side of a process boundary the fixture cannot fake
// from inside: the chat process must publish its own identity at arm time, the
// `bash` tool must pass the environment through unmodified, the `ui_state` child
// must read it, and the artifact it answers with must be the one its OWN parent
// wrote. A test that set the variable itself and called the verb would prove the
// verb reads an environment variable — not that a run's own children inherit one,
// which is the whole mechanism.
//
// THE SHAPE OF THE RUN. One profile (one GopiBin, one GOPI_HOME), one chat, one
// `--dump-screen` (the default path, which lands in this run's session
// directory), and the operator's own line asking the model to read its screen
// back. The mock model does exactly one thing: it calls `bash` with the verb. So
// the assertion is made on the TOOL RESULT the chat process received from its own
// child — the artifact path, the pid it named, and the claim sentence that says
// how it was identified.
//
// The session directory is discovered from the session the run wrote, never
// guessed: the dump's location and the run's identity both derive from it.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
)

const (
	// surfIdentAsk is the operator's line — the only line in this run that a human
	// would have typed. The mock answers it with a tool call, never with prose.
	surfIdentAsk = "read your own screen back with the ui_state verb"
	// surfIdentToolCallID names the single tool call the mock makes.
	surfIdentToolCallID = "call_surface_identity_01"
	// surfIdentAnswer is the model's closing line once the tool result is in, so
	// the run ends on a turn boundary rather than on the tool round.
	surfIdentAnswer = "SURFACE-IDENTITY-READ"
	// surfIdentDumpFlag is the flag that makes this run write its own artifact —
	// the SAME spelling the product advertises (chatDumpFlag lives in package
	// main; its value is asserted there, and spelled here the way the CLI's own
	// usage page spells it).
	surfIdentDumpFlag = "--dump-screen"
	// surfIdentTargetChat is the target value the verb is asked for, so the run
	// reads the CHAT surface's artifact instead of any other surface's.
	surfIdentTargetChat = "chat"
	// surfIdentVerbTemplate is the shell command the `bash` tool is given, with the
	// verb's own argv0 filled in at run time (the fixture's build, by ABSOLUTE
	// path: the point of this case is what the CHILD inherits, and a run whose
	// child depended on the developer's PATH would be a different test — a
	// missing `gopi` would read as a failure of the identity channel). The call
	// carries NO --pid and NO --session: the caller names nothing, which is
	// exactly the case this epic is about, so its own identity is the only thing
	// that can answer the question.
	//
	// WHY THE OUTPUT IS NARROWED, and why that is not the case cheating. The tool
	// result is TRUNCATED at a fixed byte budget, keeping the TAIL (measured on
	// this very run: the result arrived as `[output truncated]` with the grid's
	// 24 rows intact and the observation's header — claim sentence, surface, pid,
	// path — cut off the top). The evidence this case is about IS the head, so the
	// pipeline keeps it; the verb still runs with its real arguments against the
	// real ledger, and `sed` only decides which of its bytes the assertion sees.
	// The header line is what a reader would read first, so nothing is hidden that
	// the reader would have needed.
	surfIdentVerbTemplate = "set -o pipefail; %s ui_state --target " + surfIdentTargetChat + " | sed -n '1," + surfIdentHeadLines + "p'"
	// surfIdentHeadLines is how many lines of the verb's output the child keeps —
	// the observation's head (see surfIdentVerbTemplate for why the head is the
	// part that must survive).
	surfIdentHeadLines = "4"
	// surfIdentRecordName is the session record's file name, used only to find the
	// run's own session directory (the layout, not a second definition of it: the
	// glob below matches whatever the layout writes).
	surfIdentRecordGlob = "sessions/*/session.jsonl"
)

// TestChatSurfaceIdentity_ReachesTheVerbItsChildrenRun is the acceptance: the
// verb, spawned from inside this run, answers with THIS run's own artifact and
// says by which half of the identity it was identified.
func TestChatSurfaceIdentity_ReachesTheVerbItsChildrenRun(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   surfIdentToolCallID,
			Name: "bash",
			Args: map[string]interface{}{"command": fmt.Sprintf(surfIdentVerbTemplate, strconv.Quote(testutil.GopiBin(t)))},
		}},
		testutil.Resp{Text: surfIdentAnswer},
	)
	defer llm.Close()

	// The posture is the same one chat_ground_test.go declares, for the same
	// reason: this case makes the model run a `gopi ui_state` shell command, and
	// the built-in chat-policy table is `prompt *` — a PTY nobody is typing into
	// cannot answer, so the command returns `[NEEDS OPERATOR APPROVAL — command
	// NOT run]` and the artifact the assertion reads is never touched. Measured
	// on a PRISTINE HEAD extract (/dev/shm/head-6435dfd): same failure, the
	// refusal sentence verbatim in the tool result.
	chat := startPTYChat(t, []string{"chat", "--tools", surfIdentDumpFlag, chatGroundPolicyArg},
		testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.writeGapped(t, surfIdentAsk)
	chat.write(t, "\r")
	chat.waitFor(t, surfIdentAnswer, ptyPromptTimeout)

	// WHERE THE CHILD'S ANSWER IS READ, and why not the console: the turn's tool
	// result is folded into the transcript (the grid shows `⚙ call bash …` and
	// `↳ …`, a folded line), so the child's OUTPUT is not on the screen at all —
	// it is in the session record the run wrote, which is what the model was
	// given and therefore the least-mediated copy of the evidence. Reading it
	// there is reading the fact rather than the drawing of the fact.
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}
	toolResult := surfIdentToolResult(t, home, surfIdentToolCallID)

	// The run's OWN session directory — the operand both halves of this proof are
	// about, read from what the run actually wrote.
	sessions, err := filepath.Glob(filepath.Join(home, config.ProfileDirName, surfIdentRecordGlob))
	if err != nil {
		t.Fatalf("glob the run's session records: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("the run wrote %d session records (%v), want exactly 1", len(sessions), sessions)
	}
	sessionDir := sessionlayout.RecordContainer(sessions[0])
	dumpPath := filepath.Join(sessionDir, uiclient.ChatDumpFileName)

	// THE ARTIFACT EXISTS: the chat wrote it where this run's identity says it is.
	if _, statErr := statPath(dumpPath); statErr != nil {
		t.Fatalf("this run armed a dump but no artifact is at %s: %v\n--- tool result ---\n%s", dumpPath, statErr, toolResult)
	}
	// AND THE CHILD READ THAT VERY ARTIFACT. The verb prints `dump=<path>` on the
	// observation line, and the path must be this run's own — a child that had
	// answered with another surface's artifact, or with none, fails here.
	if !strings.Contains(toolResult, "dump="+dumpPath) {
		t.Fatalf("the verb the chat spawned did not read this run's own artifact %s\n--- claim line ---\n%s\n--- tool result ---\n%s", dumpPath, surfIdentClaimLine(toolResult), toolResult)
	}
	// THE HALF OF THE IDENTITY THAT A CHILD COULD NOT DERIVE: the pid the verb
	// reports must be the CHAT process's, and the claim sentence must say the
	// artifact was claimed rather than found by a scan.
	if !strings.Contains(toolResult, "pid="+strconv.Itoa(chat.cmd.Process.Pid)) {
		t.Errorf("the observation does not name this run's pid %d\n--- tool result ---\n%s", chat.cmd.Process.Pid, toolResult)
	}
	if !strings.Contains(toolResult, uiStateClaimNoticeMarker) {
		t.Errorf("the observation carries no claim sentence, so nothing says HOW the artifact was chosen\n--- tool result ---\n%s", toolResult)
	}
	claimNamesSession := false
	for _, spelling := range surfIdentClaimSessionSpellings {
		if strings.Contains(toolResult, spelling) {
			claimNamesSession = true
		}
	}
	if !claimNamesSession {
		t.Errorf("the claim does not name the session half it matched on\n--- claim line ---\n%s\n--- tool result ---\n%s", surfIdentClaimLine(toolResult), toolResult)
	}
	// THE PROOF THAT IT WAS THE CHILD'S OWN CHANNEL and not a value this test
	// planted: the pid the child's observation names is the CHAT PROCESS's pid,
	// which only the chat process could have published — the test never sees it
	// except through what the child inherited.
	if !strings.Contains(toolResult, "session="+sessionDir) {
		t.Errorf("the claim does not name this run's own session directory %s\n--- tool result ---\n%s", sessionDir, toolResult)
	}
}

// surfIdentToolResult reads the tool_result body the run recorded for one
// tool_use id. It is the chat process's OWN account of what its child answered,
// and the only place the child's stdout survives the folded rendering.
func surfIdentToolResult(t *testing.T, home, toolUseID string) string {
	t.Helper()
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	raw, err := os.ReadFile(sess.Path)
	if err != nil {
		t.Fatalf("read the run's session record %s: %v", sess.Path, err)
	}
	body := string(raw)
	// The result body is JSON-escaped inside the event, so the calls' own bytes
	// arrive with their newlines written as \n; unescaping only that is enough
	// to read the assertion targets, and it keeps this helper a READER rather
	// than a second parser of the session format.
	body = strings.ReplaceAll(body, `\n`, "\n")
	idx := strings.Index(body, toolUseID)
	if idx < 0 {
		t.Fatalf("the session record does not mention the tool call %s", toolUseID)
	}
	tail := body[idx:]
	if !strings.Contains(tail, "tool_result") {
		t.Fatalf("the tool call %s produced no result in the session record", toolUseID)
	}
	return tail
}

// uiStateClaimNoticeMarker is the claim sentence's own opening, spelled here
// because the case's subject is the OBSERVABLE sentence (the constants live in
// package main, which this compiled binary is the product of — a case that
// imported them would be asserting the source against itself).
const uiStateClaimNoticeMarker = "#gopi-ui-state-claim"

// surfIdentClaimSessionSpellings are the two ways the claim names the SESSION
// half: alone, when only the directory answered; beside the pid, when both did.
// The case accepts either, and requires at least one — a child that had claimed by
// PID alone would be evidence of a different rule than the one under test (the pid
// is never the fact the child could not derive: it IS the child's parent).
var surfIdentClaimSessionSpellings = []string{"by=session", "by=pid+session"}

// claimNoticeRE matches the claim sentence, so a failure can quote the exact
// wording the child produced rather than a yes/no about a substring.
var claimNoticeRE = regexp.MustCompile(`#gopi-ui-state-claim[^\\]*`)

// surfIdentClaimLine is the claim sentence as it appeared, for a failure message.
func surfIdentClaimLine(body string) string {
	if m := claimNoticeRE.FindString(body); m != "" {
		return m
	}
	return "(no claim sentence found)"
}

// flattenIdent removes every whitespace character, the same normalisation the pty
// harness's other cases use: a notice longer than the pty's width WRAPS across
// rows, and the wrap point is the terminal's business, not the assertion's.
func flattenIdent(s string) string { return strings.Join(strings.Fields(s), "") }

// statPath is a one-line existence probe, so the failure message can carry the
// path it looked for (a bare os.Stat would report the reason without the target
// in every call site).
func statPath(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return fi.Size(), nil
}
