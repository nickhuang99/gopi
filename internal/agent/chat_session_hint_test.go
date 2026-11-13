package agent

// chat_session_hint_test.go — the per-RUN half of the chat prompt: the
// [CURRENT SESSION] block that names THIS process's own transcript FILE.
//
// WHY THIS FILE IS SEPARATE from chat_context_test.go. That file covers the
// STATIC half (host · node · repo · the self-observation loop), which is a
// property of the process at large. This one covers the half that only a
// RUNNING process can know: which raw HTTP log and which session JSONL it is
// writing right now. The two have different failure modes — the static half can
// be wrong (a hard-coded host), the runtime half can be MISSING (a prompt that
// names the verbs but never the file) — and the second is the defect this file
// catches: a chat asked "which file settles what you actually sent?" could only
// guess, and a chat that guessed denied a file that existed.
//
// THE THREE ARMS are the three states that really occur, and each is driven here
// through the ASSEMBLED prompt — the string the request builder sends
// (chatSystemPromptServed / chatOpsSystemPrompt), never a copy of the text:
//
//  1. both logs live   → both named, with this run's REAL paths;
//  2. session only     → the wire log reported ABSENT, with the switch that
//     explains it (GOPI_RAW_LOG=OFF);
//  3. neither          → "no transcript of this run is on disk", plus the
//     instruction to say so rather than name a file.
//
// The fixtures are built through the PRODUCTION constructors (NewRawLoggerForRun /
// NewSessionLogger) rather than by setting path fields, so a block reading a field
// nobody populates fails here instead of passing on a hand-made shape.
//
// Rule 17: every env flip is t.Setenv (test-scoped).

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
)

// chatHintRunUUID is the fixed run uuid of the wire-log arm, so a failure message
// can be read against the path it names.
const chatHintRunUUID = "11111111-2222-3333-4444-555555555555"

// chatHintAgent builds an Agent whose logs are REAL files under the test's
// scratch home. rawOff selects arm 2: the raw logger is created with GOPI_RAW_LOG
// set to its OFF value, which returns (nil, nil) by design — the real shape of a
// run started with the wire log switched off, not a fabricated nil field.
func chatHintAgent(t *testing.T, rawOff bool) *Agent {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_RAW_LOG_DIR", "")
	if rawOff {
		t.Setenv("GOPI_RAW_LOG", rawLogDisabledValue)
	} else {
		t.Setenv("GOPI_RAW_LOG", "")
	}

	raw, err := NewRawLoggerForRun("", chatHintRunUUID)
	if err != nil {
		t.Fatalf("NewRawLoggerForRun: %v", err)
	}
	if rawOff && raw != nil {
		t.Fatalf("precondition: %s=%s must yield no raw logger, got %q",
			rawLogEnableEnv, rawLogDisabledValue, raw.Path())
	}
	if !rawOff && raw == nil {
		t.Fatal("precondition: the wire log must be on for this arm")
	}

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() {
		if raw != nil {
			raw.Close() //stylegate:ignore test teardown: RawLogger.Close flushes the audit log, best-effort by design, and no assertion in this file reads the flushed bytes
		}
		sl.Close() //stylegate:ignore test teardown: SessionLogger.Close flushes the JSONL, best-effort by design, and this file asserts on the prompt rather than the file's contents
	})
	return &Agent{RawLog: raw, SessionLog: sl}
}

// chatHintPaths returns the two paths the prompt must name for an agent, read
// from the loggers themselves (never re-derived from the uuid layout).
func chatHintPaths(a *Agent) (rawPath, sessionPath string) {
	if a.RawLog != nil {
		rawPath = a.RawLog.Path()
	}
	if a.SessionLog != nil {
		sessionPath = a.SessionLog.Path()
	}
	return rawPath, sessionPath
}

// chatHintPaths is driven on BOTH chat paths for every arm, because the defect
// this epic closes is precisely a half-served pair: one path told the run where
// its own transcript is and the other not. The table is the SAME two entries each
// time so a path cannot be covered by accident in one arm and missed in another.
func chatHintPathsUnder(t *testing.T, a *Agent) []struct {
	name   string
	prompt string
} {
	t.Helper()
	return []struct {
		name   string
		prompt string
	}{
		{"plain (Agent.Chat)", chatSystemPromptServed(a)},
		{"tools (Agent.ChatOps)", chatOpsSystemPrompt(a)},
	}
}

// TestChatPromptArmBothLogs drives arm 1 on both paths: the assembled prompt names
// this run's raw HTTP log and its session JSONL, and hands the model the one
// invocation that prints the wire log.
func TestChatPromptArmBothLogs(t *testing.T) {
	a := chatHintAgent(t, false)
	rawPath, sessionPath := chatHintPaths(a)
	if rawPath == "" || sessionPath == "" {
		t.Fatalf("precondition: both loggers must carry a path (raw=%q session=%q)", rawPath, sessionPath)
	}

	for _, tc := range chatHintPathsUnder(t, a) {
		if !strings.Contains(tc.prompt, chatSessionBlockMarker) {
			t.Fatalf("%s: the assembled prompt carries NO per-run session block:\n%s", tc.name, tc.prompt)
		}
		for _, want := range []string{rawPath, sessionPath, a.SessionLog.SessionID()} {
			if !strings.Contains(tc.prompt, want) {
				t.Errorf("%s: the prompt does not name %q — the run must be told where its own record is:\n%s",
					tc.name, want, tc.prompt)
			}
		}
		// The invocation, built from the leaf constants, carrying THIS path.
		inv := cliverbs.Raw + " " + cliverbs.FileFlag + " " + rawPath
		if !strings.Contains(tc.prompt, inv) {
			t.Errorf("%s: the prompt does not render the invocation %q:\n%s", tc.name, inv, tc.prompt)
		}
		// Arm 1 must carry NEITHER absence sentence: a run with both artifacts
		// told that one is missing would deny a file that exists.
		for _, absent := range []string{chatSessionNoRawLogSentence, chatSessionNoTranscriptSentence} {
			if strings.Contains(tc.prompt, absent) {
				t.Errorf("%s: the prompt claims %q while both artifacts exist:\n%s", tc.name, absent, tc.prompt)
			}
		}
	}
}

// TestChatPromptArmSessionOnly drives arm 2: a session record with no wire log.
// The prompt must name the session JSONL AND report the absent wire log together
// with the switch that caused it — an absent artifact is reported, never left for
// the model to infer (or invent).
func TestChatPromptArmSessionOnly(t *testing.T) {
	a := chatHintAgent(t, true)
	rawPath, sessionPath := chatHintPaths(a)
	if rawPath != "" {
		t.Fatalf("precondition: arm 2 must have no raw logger, got %q", rawPath)
	}
	if sessionPath == "" {
		t.Fatal("precondition: arm 2 must still have a session logger")
	}

	for _, tc := range chatHintPathsUnder(t, a) {
		if !strings.Contains(tc.prompt, sessionPath) {
			t.Errorf("%s: arm 2 must still name the session JSONL %q:\n%s", tc.name, sessionPath, tc.prompt)
		}
		if !strings.Contains(tc.prompt, chatSessionNoRawLogSentence) {
			t.Errorf("%s: arm 2 must report the ABSENT wire log:\n%s", tc.name, tc.prompt)
		}
		if !strings.Contains(tc.prompt, rawLogEnableEnv+"="+rawLogDisabledValue) {
			t.Errorf("%s: arm 2 must name the switch that explains the absence (%s=%s):\n%s",
				tc.name, rawLogEnableEnv, rawLogDisabledValue, tc.prompt)
		}
		if strings.Contains(tc.prompt, chatSessionNoTranscriptSentence) {
			t.Errorf("%s: arm 2 HAS a transcript (the session JSONL) but claims none:\n%s", tc.name, tc.prompt)
		}
	}
}

// TestChatPromptArmNeitherLog covers arm 3 — the fail-safe. A run whose BOTH
// loggers failed to attach (an Agent with no transcript, NOT a nil Agent: nil
// means "no run at all" and is the removable proof below) is told there is no
// transcript of this run AND told to say so rather than name a file; this arm's
// opposite (naming a file that does not exist) is the failure the block was
// written to end.
func TestChatPromptArmNeitherLog(t *testing.T) {
	transcriptless := &Agent{}
	for _, tc := range chatHintPathsUnder(t, transcriptless) {
		if !strings.Contains(tc.prompt, chatSessionNoTranscriptSentence) {
			t.Errorf("%s: arm 3 must say there is no transcript of this run:\n%s", tc.name, tc.prompt)
		}
		if !strings.Contains(tc.prompt, chatEntityUnknown) {
			t.Errorf("%s: arm 3 must still identify the run it is speaking about (%q):\n%s",
				tc.name, chatEntityUnknown, tc.prompt)
		}
		// The STATIC half is not lost in this arm — only the runtime half degrades.
		if !strings.Contains(tc.prompt, chatContextHead) {
			t.Errorf("%s: arm 3 dropped the chat-context block itself:\n%s", tc.name, tc.prompt)
		}
	}
}

// TestChatPromptSessionBlockIsRemovable is the red proof that the arms above
// assert a REMOVABLE thing rather than a tautology: with no Agent (the regression
// shape — a run that forgets to hand its own logs to the assembly) the assembled
// prompt loses the per-run block on BOTH paths and keeps the static half. It is
// the same property the mutate-and-rerun acceptance check exercises at the source
// level: delete the append, and these arms go red.
func TestChatPromptSessionBlockIsRemovable(t *testing.T) {
	a := chatHintAgent(t, false)
	for _, tc := range chatHintPathsUnder(t, a) {
		if !strings.Contains(tc.prompt, chatSessionBlockMarker) {
			t.Fatalf("precondition: %s must carry the block before it is removed:\n%s", tc.name, tc.prompt)
		}
	}
	for _, tc := range chatHintPathsUnder(t, nil) {
		if strings.Contains(tc.prompt, chatSessionBlockMarker) {
			t.Errorf("%s: the block survived a nil Agent — the per-run half must depend on the run:\n%s",
				tc.name, tc.prompt)
		}
		if !strings.Contains(tc.prompt, chatContextHead) {
			t.Errorf("%s: dropping the runtime half must not drop the static block:\n%s", tc.name, tc.prompt)
		}
	}
}

// TestChatPromptNamesThisProcesssOwnPaths is the TRUTH half, independent of the
// renderer: the paths the prompt names are the ones the loggers resolved, both
// exist on disk, and a SECOND run renders ITS paths rather than the first one's.
// That last assertion is what shows the block reads the run it was handed rather
// than any package-level state.
func TestChatPromptNamesThisProcesssOwnPaths(t *testing.T) {
	a := chatHintAgent(t, false)
	rawPath, sessionPath := chatHintPaths(a)

	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("the raw logger's own path does not exist: %v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("the session logger's own path does not exist: %v", err)
	}

	b := chatHintAgent(t, false)
	rawPathB, _ := chatHintPaths(b)
	if rawPathB == rawPath {
		t.Fatalf("two runs resolved the same raw path %q — the fixture cannot distinguish them", rawPath)
	}
	for _, tc := range chatHintPathsUnder(t, b) {
		if !strings.Contains(tc.prompt, rawPathB) {
			t.Errorf("%s: the prompt omits the SECOND run's raw path %q:\n%s", tc.name, rawPathB, tc.prompt)
		}
		if strings.Contains(tc.prompt, rawPath) {
			t.Errorf("%s: the prompt rendered the FIRST run's raw path %q — it is not reading the run it was handed:\n%s",
				tc.name, rawPath, tc.prompt)
		}
	}
}

// TestChatPromptCarriesMastersHonestySentenceNotTheDeletedProhibition pins the
// ruling this epic had to respect, on the ASSEMBLED plain-path prompt rather
// than on the frozen const. The archived branch carried a scoped *"do not
// mention tools, task lists, or system internals unless asked"* sentence; master
// DELETED it as the sentence that MADE the defect (it forbade the surface from
// saying what the chat-context block now TELLS it) and replaced it, in the same
// slot, with the honesty sentence. Both halves are asserted here so a future
// "recovery" of that prohibition fails red against a landed ruling instead of
// silently re-pinning the defect — the master replacement is asserted as the
// present text, and every substring of the deleted sentence is asserted absent.
func TestChatPromptCarriesMastersHonestySentenceNotTheDeletedProhibition(t *testing.T) {
	a := chatHintAgent(t, false)
	body := chatBodyOf(chatSystemPromptServed(a))

	if !strings.Contains(body, masterHonestySentence) {
		t.Errorf("the plain chat persona body lost master's honesty sentence %q:\n%s", masterHonestySentence, body)
	}
	// The deleted prohibition, in each of the fragments the archived branch's
	// version would have re-introduced. Any one of them coming back is the
	// defect master's amendment comment names.
	for _, forbidden := range []string{
		"Do not mention tools, task lists, or system internals unless asked",
		"unless asked",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the persona body carries the DELETED prohibition %q — master removed it as the sentence "+
				"that MADE the defect (see the freezePreKnobChatPrompt amendment note):\n%s", forbidden, body)
		}
	}
	// And the surfaces the prohibition forbade are named on this very path —
	// which is why re-adding it would contradict the landed ruling.
	for _, named := range []string{chatContextHead, chatSessionBlockMarker} {
		if !strings.Contains(chatSystemPromptServed(a), named) {
			t.Errorf("the plain path no longer names %q, so the deleted prohibition would not even contradict "+
				"anything — the test's premise is broken", named)
		}
	}
}

// masterHonestySentence is master's OWN replacement for the deleted prohibition,
// in the same slot of the persona body. It is spelled once here (Rule 13) and
// asserted against the assembled prompt.
const masterHonestySentence = "Honesty over fluency: never claim to have seen, run or checked something you have not."

// TestChatPromptSessionBlockByteStableTurnToTurn pins the prompt-cache contract
// for the half that reads live process state: two consecutive renders with nothing
// changed in between produce the SAME bytes, so the per-turn re-render cannot
// invalidate the cache by itself.
func TestChatPromptSessionBlockByteStableTurnToTurn(t *testing.T) {
	a := chatHintAgent(t, false)
	for _, tc := range chatHintPathsUnder(t, a) {
		again := tc.prompt
		if tc.name == "plain (Agent.Chat)" {
			again = chatSystemPromptServed(a)
		} else {
			again = chatOpsSystemPrompt(a)
		}
		if tc.prompt != again {
			t.Errorf("%s: the assembled prompt changed between two consecutive renders — the runtime half is not byte-stable",
				tc.name)
		}
	}
}
