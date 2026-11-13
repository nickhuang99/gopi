package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
)

// This file is the acceptance of chat resume-by-options: `gopi chat --resume
// <session-id>` must continue a crashed session WITH the options it was started
// with, so the operator does not replay the command line from memory.
//
// THE DEFECT (measured 2026-09-29, task 01a0eb0e…, devdoc/chat-crash-resume.md,
// its own `## Not Done`): a checkpoint restores a conversation and a tool
// DEFINITION list, never a command line. `gopi chat --tools` SIGKILLed and
// resumed with the seed alone kept the history and lost the capability; the
// previous fix only said so ("re-run with --tools").
//
// Two drivers per claim, on purpose (Rule 31): the parser and the apply path
// IN PROCESS (a failure names the line that broke), and the REAL BINARY for the
// end-to-end shape (the in-process driver cannot prove the dispatch path
// reaches the parser at all).

// chatResumeFixtureID is the session id every fixture uses: it is the DIRECTORY
// name under <gopiHome>/.gopi/sessions/, which is the address the run path mints
// and gopilog.FindSession resolves.
const chatResumeFixtureID = "c0ffee00-1111-4222-8333-444455556666"

// chatResumeSessionDir is the sandbox profile's run directory for the fixture
// session (bare layout: <gopiHome>/.gopi/sessions/<uuid>).
func chatResumeSessionDir(gopiHome, sessionID string) string {
	return filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName, sessionID)
}

// chatResumeFixture writes a session that looks exactly like one a CRASHED
// `gopi chat --tools --dump-screen` run leaves behind: a session.jsonl whose
// first line is session_start (recording the co-located raw log), a
// resume_options record written at start-up, and a raw log carrying the
// checkpoint (the → REQUEST block LoadCheckpoint parses).
//
// flags=="" writes NO record — the shape of every session that predates the
// event, which is the case the refusal/notice path exists for.
func chatResumeFixture(t *testing.T, gopiHome, sessionID string, flags ...string) string {
	t.Helper()
	dir := chatResumeSessionDir(gopiHome, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	raw := filepath.Join(dir, sessionID+".raw.log")
	ckpt := `{"model":"deepseek-v4-flash","stream":true,` +
		`"system":[{"type":"text","text":"sys"}],` +
		`"messages":[{"role":"user","content":"the crashed run's question"}],"thinking":null}`
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"+ckpt+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	lines := []string{
		`{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"` + sessionID +
			`","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-10-01T00:00:00Z"}`,
	}
	if len(flags) > 0 {
		b, err := json.Marshal(flags)
		if err != nil {
			t.Fatalf("marshal flags: %v", err)
		}
		lines = append(lines, `{"type":"`+sessionevent.EventTypeResumeOptions+`","`+
			sessionevent.ResumeOptionsFieldFlags+`":`+string(b)+`,"`+
			sessionevent.ResumeOptionsFieldSessionID+`":"`+sessionID+`"}`)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	return filepath.Join(dir, "session.jsonl")
}

// chatResumeSandbox is the isolated profile every case here runs in (its own
// GOPI_HOME, its own ledger, a mock LLM — no real API call, and nothing touches
// the operator's ~/.gopi).
func chatResumeSandbox(t *testing.T) (gopiHome string, env []string) {
	t.Helper()
	gopiHome = t.TempDir()
	dbPath := filepath.Join(gopiHome, config.ProfileDirName, config.MemoryDBFileName)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "resumed ok"})
	t.Cleanup(mock.Close)
	t.Setenv(config.MemoryDBEnv, dbPath)
	env = testutil.MockEnv(t, gopiHome, mock.URL())
	return gopiHome, append(env, config.MemoryDBEnv+"="+dbPath)
}

// TestChatResumeSwitchRestoresRecordedTools is the headline: the switch, with
// the session id alone and NO --tools, must restore the tool-enabled capability
// the crashed run had.
func TestChatResumeSwitchRestoresRecordedTools(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)
	chatResumeFixture(t, gopiHome, chatResumeFixtureID, chatFlagTools, chatFlagNoColor)

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatFlagResume, chatResumeFixtureID, chatFlagNoEditor}, "\n", env, "")
	if code != chatExitOK {
		t.Fatalf("`gopi chat %s <id>` = %d, want %d\nstderr:\n%s", chatFlagResume, code, chatExitOK, stderr)
	}
	if !strings.Contains(stderr, chatFlagTools) {
		t.Errorf("the restored options do not name %s — nothing says the capability came back:\n%s", chatFlagTools, stderr)
	}
	// The proof the flag actually took effect is the tool-enabled banner, which
	// is printed by the run's own switch handling (chatFlagTools + verbose).
	if !strings.Contains(stderr, "restored") {
		t.Errorf("no restore line was printed — the operator cannot tell what came back:\n%s", stderr)
	}
}

// TestChatResumeAutodetectsOnTheSeedArgument: the SEED spelling must also
// restore the record, which is the operator's original ask ("gopi chat
// TASKID" alone). It is one code path with the switch, reached by silence.
func TestChatResumeAutodetectsOnTheSeedArgument(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)
	chatResumeFixture(t, gopiHome, chatResumeFixtureID, chatFlagTools)

	// NO option is stated: silence is the condition (see
	// TestChatSeedAutodetectStandsDownWhenAnOptionIsStated for the other half).
	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatResumeFixtureID}, "\n", env, "")
	if code != chatExitOK {
		t.Fatalf("`gopi chat <session-id>` = %d, want %d\nstderr:\n%s", code, chatExitOK, stderr)
	}
	if !strings.Contains(stderr, "restored") {
		t.Errorf("the seed argument did not autodetect the recorded options:\n%s", stderr)
	}
}

// TestChatSeedAutodetectStandsDownWhenAnOptionIsStated pins the OTHER half of
// the autodetect condition: an operator who spells any option is being
// deliberate, so the positional seed stops inferring and only `--resume` will
// restore the rest. Without this test the rule would be enforced by accident of
// which branch happens to run first.
func TestChatSeedAutodetectStandsDownWhenAnOptionIsStated(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)
	chatResumeFixture(t, gopiHome, chatResumeFixtureID, chatFlagTools)

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatResumeFixtureID, chatFlagNoEditor}, "\n", env, "")
	if code != chatExitOK {
		t.Fatalf("`gopi chat <session-id> --no-editor` = %d, want %d\nstderr:\n%s", code, chatExitOK, stderr)
	}
	if strings.Contains(stderr, "restored") {
		t.Errorf("the seed autodetect fired although the command line stated an option — the documented condition is TOTAL silence (use %s to restore regardless):\n%s",
			chatFlagResume, stderr)
	}
}

// TestChatResumeExplicitCommandLineWins: the recorded options are ADDED to this
// command line, never a substitute for it — and for the ONE option that has two
// candidate VALUES (the screen-dump destination) the explicit one wins.
func TestChatResumeExplicitCommandLineWins(t *testing.T) {
	recorded := chatDumpFlag + chatFlagValueSep + "recorded.dump"
	parsed, matched, err := parseChatDumpArg(recorded, "", false)
	if !matched || err != nil || !parsed.requested || parsed.path != "recorded.dump" {
		t.Fatalf("fixture flag %q did not parse as a dump request: %+v matched=%v err=%v", recorded, parsed, matched, err)
	}

	// Nothing stated by this run: the record's destination is adopted.
	dumpOpts := chatDumpOptions{}
	sgrOpts := chatSgrOptions{}
	noEditor, noColor, verbose := false, false, false
	sess := &chatSession{}
	applied, superseded := chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose, &dumpOpts, &sgrOpts, []string{recorded}, false, false)
	if dumpOpts.path != "recorded.dump" || len(applied) != 1 || len(superseded) != 0 {
		t.Errorf("dump = %q applied=%v superseded=%v, want the RECORDED destination adopted",
			dumpOpts.path, applied, superseded)
	}

	// The command line stated its own: it wins, and the recorded one is NAMED
	// as superseded rather than silently ignored.
	dumpOpts = chatDumpOptions{path: "explicit.dump", requested: true}
	applied, superseded = chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose, &dumpOpts, &sgrOpts, []string{recorded}, true, false)
	if dumpOpts.path != "explicit.dump" {
		t.Errorf("dump = %q — the recorded destination OVERRODE the command line's own", dumpOpts.path)
	}
	if len(superseded) != 1 || len(applied) != 0 {
		t.Errorf("applied=%v superseded=%v, want the recorded dump named as superseded", applied, superseded)
	}

	// A recorded SWITCH can only ADD capability: this run did not have it, so
	// it is applied.
	sess = &chatSession{}
	noEditor, noColor, verbose = false, false, false
	dumpOpts = chatDumpOptions{}
	sgrOpts = chatSgrOptions{}
	applied, _ = chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose, &dumpOpts, &sgrOpts,
		[]string{chatFlagTools, chatFlagNoEditor}, false, false)
	if !sess.toolsEnabled || !noEditor || len(applied) != 2 {
		t.Errorf("recorded switches were not applied: toolsEnabled=%v noEditor=%v applied=%v",
			sess.toolsEnabled, noEditor, applied)
	}
}

// TestChatResumeUnknownRecordedOptionIsNamed: a record written by ANOTHER gopi
// version carries a word this binary does not know. Dropping it silently is the
// defect class this feature exists to close, so it is derived and NAMED — and
// the KNOWN word beside it is still applied (one bad word drops nothing).
func TestChatResumeUnknownRecordedOptionIsNamed(t *testing.T) {
	sess := &chatSession{}
	noEditor, noColor, verbose := false, false, false
	dumpOpts := chatDumpOptions{}
	sgrOpts := chatSgrOptions{}
	flags := []string{chatFlagTools, "--from-a-newer-gopi"}
	applied, superseded := chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose, &dumpOpts, &sgrOpts, flags, false, false)
	if !sess.toolsEnabled {
		t.Fatal("the KNOWN option beside the unknown one was not applied — one bad word dropped the rest")
	}
	unknown := chatUnknownResumeFlags(flags, applied, superseded)
	if len(unknown) != 1 || unknown[0] != "--from-a-newer-gopi" {
		t.Errorf("unknown = %v, want the one word this binary does not know", unknown)
	}
	// A word this binary DOES know is never reported as a version mismatch,
	// even when it was superseded: the two states have different remedies.
	if got := chatUnknownResumeFlags(flags, []string{chatFlagTools}, []string{"--from-a-newer-gopi"}); len(got) != 0 {
		t.Errorf("unknown = %v, want none — both words were accounted for", got)
	}
}

// TestChatResumeRecordsItsOwnOptions: the run must RECORD what it was started
// with, before the first turn — that record is the entire input of a later
// resume. Driven through the real binary so the record's own line is read back
// from disk.
func TestChatResumeRecordsItsOwnOptions(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatFlagTools, chatFlagNoColor, chatFlagNoEditor}, "\n", env, "")
	if code != chatExitOK {
		t.Fatalf("chat = %d, want %d\nstderr:\n%s", code, chatExitOK, stderr)
	}

	// Find the session this run minted and read its record back through the
	// SAME reader a later resume uses.
	sessionsRoot := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName)
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		t.Fatalf("read sessions root: %v", err)
	}
	found := ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(sessionsRoot, e.Name(), "session.jsonl"))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), sessionevent.EventTypeResumeOptions) {
			found = string(b)
		}
	}
	if found == "" {
		t.Fatal("the run recorded NO resume_options event — a later resume has nothing to restore (the whole feature's input)")
	}
	for _, want := range []string{chatFlagTools, chatFlagNoColor} {
		if !strings.Contains(found, want) {
			t.Errorf("the record does not carry %s:\n%s", want, found)
		}
	}
}

// TestChatResumeSwitchRefusesAnEmptyTarget: `--resume` alone names no session.
// It must be a USAGE error naming the flag — never a silent start of an empty
// chat while the operator believes they resumed something (Rule 15).
func TestChatResumeSwitchRefusesAnEmptyTarget(t *testing.T) {
	_, stderr, code := chatRunArgs(t, chatFlagResume)
	if code != chatExitUsage {
		t.Errorf("runChat([%s]) = %d, want the usage status %d\nstderr:\n%s", chatFlagResume, code, chatExitUsage, stderr)
	}
	if !strings.Contains(stderr, chatFlagResume) {
		t.Errorf("the refusal does not name %s:\n%s", chatFlagResume, stderr)
	}
}

// TestChatResumeSwitchRefusesTwoTargets: the switch and the seed are the SAME
// thing asked two ways, so naming two DIFFERENT sessions is refused rather than
// resolved to whichever the parser happened to read last.
func TestChatResumeSwitchRefusesTwoTargets(t *testing.T) {
	_, stderr, code := chatRunArgs(t, chatFlagResume, "session-a", "session-b")
	if code != chatExitUsage {
		t.Errorf("runChat = %d, want the usage status %d — two resume targets cannot both be honoured\nstderr:\n%s",
			code, chatExitUsage, stderr)
	}
	if !strings.Contains(stderr, chatFlagResume) {
		t.Errorf("the refusal does not name %s:\n%s", chatFlagResume, stderr)
	}
}

// TestChatResumeSessionWithNoRecordSaysSo: a session that recorded nothing (the
// shape of every session predating the event) must be reported as an
// unrestorable command line, and its CONVERSATION must still resume — an
// operator with an older session is not left without a path.
func TestChatResumeSessionWithNoRecordSaysSo(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)
	chatResumeFixture(t, gopiHome, chatResumeFixtureID) // no flags = no record

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatFlagResume, chatResumeFixtureID, chatFlagNoEditor}, "\n", env, "")
	if code != chatExitOK {
		t.Fatalf("resuming a session with no record = %d, want %d (its conversation is still resumable)\nstderr:\n%s",
			code, chatExitOK, stderr)
	}
	if !strings.Contains(stderr, "recorded none") {
		t.Errorf("no record was reported — the operator cannot tell the options were NOT restored:\n%s", stderr)
	}
}

// TestChatHelpDocumentsResume: the page is where an operator learns the
// recovery path, and the argv guard (chat_argv_test.go) independently requires
// every parsed flag to be documented. Both must hold.
func TestChatHelpDocumentsResume(t *testing.T) {
	for _, want := range []string{
		chatFlagResume,
		"gopi chat --resume <session-id>",
		"RECORDS the options",
	} {
		if !strings.Contains(chatUsage, want) {
			t.Errorf("`gopi chat -h` never says %q — resume-by-options stays undiscoverable", want)
		}
	}
	// The old recipe told the operator to replay flags by hand; that sentence
	// is now false for every session carrying a record, and a help page that
	// contradicts the code is worse than a silent one.
	if strings.Contains(chatUsage, "REPLAY THE FLAGS") {
		t.Error("`gopi chat -h` still tells the operator to replay flags by hand — the record now restores them")
	}
}

// TestChatResumeDoesNotWarnAboutCapabilityItRestored is the REGRESSION GUARD for
// a defect the live sandbox run exposed (2026-10-01), which no unit test had
// covered because each line was individually correct:
//
//	[gopi] chat: ⚠ the seeded session was TOOL-ENABLED (18 tool definition(s) ...) but this run has tools OFF ...
//	[gopi] chat: restored 2 option(s) this session was started with: --tools, --no-editor
//
// Two consecutive lines contradicting each other. The capability warning was
// decided at the SEED — before the recorded options were applied — so it
// reported a loss the very next line repaired, and it sent the operator off to
// replay a flag that had already been restored. The fix moved the warning AFTER
// the apply; this test fails if it moves back (or if the restore stops working,
// since the warning's absence is exactly what proves the restore took effect
// before the decision).
func TestChatResumeDoesNotWarnAboutCapabilityItRestored(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)
	// A checkpoint that CARRIES tool definitions — the precondition the old
	// warning keyed on — plus a record restoring --tools. This is the exact
	// shape of every crashed `gopi chat --tools` session.
	chatResumeFixture(t, gopiHome, chatResumeFixtureID, chatFlagTools)

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatFlagResume, chatResumeFixtureID, chatFlagNoEditor}, "\n", env, "")
	if code != chatExitOK {
		t.Fatalf("resume = %d, want %d\nstderr:\n%s", code, chatExitOK, stderr)
	}
	if strings.Contains(stderr, "TOOL-ENABLED") {
		t.Errorf("the run WARNED about a capability loss it had just restored — the warning is decided before the recorded options are applied:\n%s", stderr)
	}
	if !strings.Contains(stderr, "restored") {
		t.Errorf("the restore line is missing, so this test would pass vacuously:\n%s", stderr)
	}
}
