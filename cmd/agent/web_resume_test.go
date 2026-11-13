package main

// web_resume_test.go — the `gopi web <session-id>` continuation, pinned at the
// seams it must not drift from:
//
//	1. the PARSER collects a bare word as a session address (and does NOT eat it
//	   as a flag value);
//	2. the DISPATCHER routes `gopi web <bare word>` to the SERVER rather than
//	   refusing it as an unknown tool subcommand — while `gopi web search` stays
//	   a tool subcommand;
//	3. the SEED resolves a real session on disk to THAT session's id, raw log and
//	   history, takes the run lock, and refuses an address that names no session
//	   with the reason (never by silently starting a fresh one);
//	4. the recorded options are restored ONLY for the switches this command line
//	   did not state, and each restore is reported.
//
// The fixtures are the ones chat_resume_options_test.go already builds — the
// same session a crashed `gopi chat --tools` run leaves behind — because what is
// under test is "does the web face read what the chat face wrote" (Rule 14: one
// fixture, not a second opinion about what a session looks like on disk).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
)

// TestWebParseFlagsCollectsTheSessionAddress — a bare word is an ADDRESS.
func TestWebParseFlagsCollectsTheSessionAddress(t *testing.T) {
	opts, err := webParseFlags([]string{chatResumeFixtureID, webToolsFlag}, ":8090", false, false)
	if err != nil {
		t.Fatalf("webParseFlags: %v", err)
	}
	if len(opts.Positional) != 1 || opts.Positional[0] != chatResumeFixtureID {
		t.Fatalf("Positional = %v, want [%s]", opts.Positional, chatResumeFixtureID)
	}
	if !opts.Tools {
		t.Fatalf("%s was not applied", webToolsFlag)
	}
	if opts.Addr != ":8090" {
		t.Fatalf("Addr = %q, want :8090 (the space-separated --addr value must survive the positional)", opts.Addr)
	}
}

// TestWebGroupRoutesABareWordToTheServer — `gopi web <id>` is the server;
// `gopi web search` is still the tool subcommand.
func TestWebGroupRoutesABareWordToTheServer(t *testing.T) {
	if webGroupServerFlags(chatResumeFixtureID) {
		t.Fatalf("a session address was classified as a SERVER FLAG")
	}
	if !webGroupAddressOperand([]string{chatResumeFixtureID}) {
		t.Fatalf("a session address was not recognised as an address operand — the dispatcher would refuse it as an unknown subcommand")
	}
	if webGroupAddressOperand([]string{webGroupSubcmdSearch}) {
		t.Fatalf("%s was classified as a session address — the tool subcommand would never run", webGroupSubcmdSearch)
	}
	if webGroupAddressOperand([]string{webToolsFlag}) {
		t.Fatalf("a flag word was classified as a session address")
	}
	if webGroupAddressOperand(nil) {
		t.Fatalf("an empty argv tail was classified as a session address (bare `gopi web` must stay the server)")
	}
}

// TestWebResumeSeedResolvesTheExistingSession — the seed names the session on
// disk, seeds its history, and holds the run lock.
func TestWebResumeSeedResolvesTheExistingSession(t *testing.T) {
	gopiHome, _ := chatResumeSandbox(t)
	// The profile this process reads is the FIXTURE's: chatResumeSandbox hands
	// the isolated home back to the caller rather than setting it in-process, so
	// the in-process resolver (config.GopiDir, which the seed goes through) is
	// pointed at it here — without this the seed would read the operator's own
	// profile and find nothing.
	t.Setenv(config.GopiDirEnv, gopiHome)
	sessionFile := chatResumeFixture(t, gopiHome, chatResumeFixtureID, webToolsFlag, webThinkingFlag)

	seed, err := webResumeSeedFor(chatResumeFixtureID)
	if err != nil {
		t.Fatalf("webResumeSeedFor(%s): %v", chatResumeFixtureID, err)
	}
	if seed.Lock != nil {
		defer seed.Lock()
	}
	if seed.SessionID != chatResumeFixtureID {
		t.Fatalf("SessionID = %q, want the continued session %q (a resume must not mint)", seed.SessionID, chatResumeFixtureID)
	}
	if seed.SessionLogPath != sessionFile {
		t.Fatalf("SessionLogPath = %q, want %q", seed.SessionLogPath, sessionFile)
	}
	if !strings.HasSuffix(seed.RawLogPath, chatResumeFixtureID+".raw.log") {
		t.Fatalf("RawLogPath = %q, want the session's own raw log", seed.RawLogPath)
	}
	if len(seed.History) == 0 {
		t.Fatalf("History is empty — the continued conversation was not seeded")
	}
	id := seed.identity()
	if id.SessionID != chatResumeFixtureID || id.ResumedFrom != chatResumeFixtureID {
		t.Fatalf("identity = %+v, want SessionID and ResumedFrom = %s", id, chatResumeFixtureID)
	}
	if id.SessionLogPath != sessionFile {
		t.Fatalf("identity.SessionLogPath = %q, want %q", id.SessionLogPath, sessionFile)
	}
	if len(seed.RecordedOptions) != 2 {
		t.Fatalf("RecordedOptions = %v, want the two switches the session recorded", seed.RecordedOptions)
	}
}

// TestWebResumeSeedRefusesAnAddressThatNamesNoSession — the refusal names its
// cause and never falls back to a fresh conversation.
func TestWebResumeSeedRefusesAnAddressThatNamesNoSession(t *testing.T) {
	gopiHome, _ := chatResumeSandbox(t)
	t.Setenv(config.GopiDirEnv, gopiHome)

	_, err := webResumeSeedFor("c0ffee00-9999-4222-8333-444455556666")
	if err == nil {
		t.Fatalf("an address naming no session was ACCEPTED — it would have reported a fresh conversation as a continuation")
	}
	if !strings.Contains(err.Error(), "cannot continue") {
		t.Fatalf("refusal = %q, want it to state what it could not continue", err)
	}
}

// TestWebApplyRecordedOptionsRestoresOnlySilence — this run's flags win; the
// record fills only what was not stated.
func TestWebApplyRecordedOptionsRestoresOnlySilence(t *testing.T) {
	recorded := []string{webToolsFlag, webThinkingFlag}
	statedNone := func(string) bool { return false }

	toolsOn, thinkingOn := webApplyRecordedOptions(recorded, statedNone, false, false)
	if !toolsOn || !thinkingOn {
		t.Fatalf("nothing was restored from the record: tools=%v thinking=%v", toolsOn, thinkingOn)
	}

	statedThinking := func(w string) bool { return w == webThinkingFlag }
	toolsOn, thinkingOn = webApplyRecordedOptions(recorded, statedThinking, false, false)
	if !toolsOn {
		t.Fatalf("%s was not restored although this run did not state it", webToolsFlag)
	}
	if thinkingOn {
		t.Fatalf("%s was restored although this run stated it — the record overrode the command line", webThinkingFlag)
	}

	toolsOn, thinkingOn = webApplyRecordedOptions([]string{"--no-editor"}, statedNone, false, false)
	if toolsOn || thinkingOn {
		t.Fatalf("a chat-only option was applied to the web face: tools=%v thinking=%v", toolsOn, thinkingOn)
	}
}

// TestWebSwitchStatedSeesBothSpellings — the restore's precedence turns on this
// fact, so it must see the joined spelling too.
func TestWebSwitchStatedSeesBothSpellings(t *testing.T) {
	if !webSwitchStated([]string{webToolsFlag}, webToolsFlag) {
		t.Fatalf("bare spelling not seen")
	}
	if !webSwitchStated([]string{webToolsFlag + "=yes"}, webToolsFlag) {
		t.Fatalf("joined spelling not seen")
	}
	if webSwitchStated([]string{"--other"}, webToolsFlag) {
		t.Fatalf("an unrelated flag was reported as stated")
	}
}

// TestWebUsageAdvertisesTheSessionAddress — the help page must PRINT the
// positional form, because a surface nobody can discover is a surface the
// operator never types (Rule 31's "advertise before you may use it").
func TestWebUsageAdvertisesTheSessionAddress(t *testing.T) {
	if !strings.Contains(webUsage, webResumeUsageSynopsis) {
		t.Fatalf("the help page does not carry the synopsis line %q", webResumeUsageSynopsis)
	}
	if !strings.Contains(webUsage, "SESSION-ID continues") {
		t.Fatalf("the help page's synopsis names SESSION-ID but never says what it does")
	}
}

// webResumeSeedToolsFixtureCheckpoint — the checkpoint body the seeded raw log
// carries. It is VERBATIM the shape the measured session 6b01dd46 left behind:
// a TOOL-ENABLED conversation (an assistant tool_use and the tool_result that
// answered it) whose checkpoint still declares no `tools` array, because the
// run that wrote it was one of the resumed generations that had none.
const webResumeSeedToolsFixtureCheckpoint = `{"model":"deepseek-flash","stream":true,` +
	`"system":[{"type":"text","text":"sys"}],` +
	`"messages":[{"role":"user","content":"run it"},` +
	`{"role":"assistant","content":[{"type":"tool_use","id":"call_00_x","name":"bash","input":{"command":"date"}}]},` +
	`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_00_x","content":"ok"}]}]}`

// webResumeSeedToolsFixture writes the session shape measured at 6b01dd46: a
// session.jsonl whose FIRST line is session_start and whose SECOND is the
// resume_options record — or, when recorded is false, NO record at all, which is
// the shape EVERY resumed generation of 6b01dd46 had (the web face has no writer
// for the event).
func webResumeSeedToolsFixture(t *testing.T, gopiHome, sessionID string, recorded bool, toolsFlags []string) {
	t.Helper()
	dir := chatResumeSessionDir(gopiHome, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	raw := filepath.Join(dir, sessionID+".raw.log")
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"+webResumeSeedToolsFixtureCheckpoint+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	lines := []string{
		`{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"` + sessionID +
			`","model":"deepseek-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-11-07T12:09:17Z"}`,
	}
	if recorded {
		b, err := json.Marshal(toolsFlags)
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
}

// TestWebResumeSeedCarriesNoEvidenceOfToolsWhenNothingWasRecorded is the
// MINIMAL REPRODUCTION of the measured capability loss (session 6b01dd46,
// 2026-11-07), pinned at the seam where the decision is taken.
//
// THE MEASUREMENT the fixture reproduces, from that session's raw log:
//
//	round  45  has(tools)=true   (17 tool definitions)
//	round  46  has(tools)=false  (0)      ← same conversation, next process
//	round  54  has(tools)=true   (17)     ← and back again
//	round  58  has(tools)=false  (0)
//
// and BOTH round 45 and round 46 carried 33 assistant tool_use blocks and 33
// tool_result blocks in their history: the conversation said "tools", the wire
// said nothing, and whether a generation had the capability was decided by
// which process resumed it — never by anything the session recorded.
//
// WHY THIS IS ONE ASSERTION AND NOT THREE. The seed carries THREE things a
// resumed run could use to recover `--tools`: (a) the resume_options record,
// (b) the checkpoint's `tools` array, (c) the history's tool_use blocks. This
// test drives the REAL resolver and asserts that on the measured shape all
// three are silent — i.e. that the web face deterministically resumes with
// tools OFF unless the operator restates the flag. That is the defect; a test
// that only checked the record would pass for the wrong reason.
func TestWebResumeSeedCarriesNoEvidenceOfToolsWhenNothingWasRecorded(t *testing.T) {
	gopiHome, _ := chatResumeSandbox(t)
	t.Setenv(config.GopiDirEnv, gopiHome)
	webResumeSeedToolsFixture(t, gopiHome, chatResumeFixtureID, false, nil)

	seed, err := webResumeSeedFor(chatResumeFixtureID)
	if err != nil {
		t.Fatalf("webResumeSeedFor(%s): %v", chatResumeFixtureID, err)
	}
	if seed.Lock != nil {
		defer seed.Lock()
	}

	// (a) no record: the web face has no writer for resume_options, so a session
	// it started carries none — the restore arm can never fire.
	if len(seed.RecordedOptions) != 0 {
		t.Fatalf("RecordedOptions = %v, want none — the fixture records no options", seed.RecordedOptions)
	}
	// (b) the checkpoint declares no tools array.
	ckpt, _, _, _, cerr := agent.LoadCheckpointWithSource(seed.RawLogPath)
	if cerr != nil {
		t.Fatalf("LoadCheckpointWithSource: %v", cerr)
	}
	if len(ckpt.Tools) != 0 {
		t.Fatalf("the fixture checkpoint declares %d tool definition(s); it must declare none to reproduce the measured shape", len(ckpt.Tools))
	}
	// (c) the history DOES carry tool_use blocks — so the conversation cannot be
	// the evidence channel either, and a resumed run has no way to infer it.
	var toolUse int
	for _, m := range seed.History {
		if len(m.ToolCalls) > 0 {
			toolUse++
		}
	}
	if toolUse == 0 {
		t.Fatalf("the fixture history carries no tool_use block — it does not reproduce the measured shape (33 such blocks in rounds 45/46)")
	}

	// THE VERDICT: with nothing recorded and nothing stated, the restore arm
	// leaves tools OFF, which is exactly what the measured rounds 46/58 show on
	// the wire.
	toolsOn, thinkingOn := webApplyRecordedOptions(seed.RecordedOptions,
		func(string) bool { return false }, false, false)
	if toolsOn || thinkingOn {
		t.Fatalf("tools restored from nothing: tools=%v thinking=%v", toolsOn, thinkingOn)
	}
}

// TestWebResumeRestoresToolsWhenTheSessionRecordedThem is the OTHER direction
// of the same seam, and the reason the fix can be a WARNING rather than a new
// default: when a record EXISTS, the web face already restores `--tools`
// without the operator restating it. So the loss is confined to sessions with
// no record — which is every session the web face itself started, because it
// never writes one.
func TestWebResumeRestoresToolsWhenTheSessionRecordedThem(t *testing.T) {
	gopiHome, _ := chatResumeSandbox(t)
	t.Setenv(config.GopiDirEnv, gopiHome)
	webResumeSeedToolsFixture(t, gopiHome, chatResumeFixtureID, true, []string{webToolsFlag})

	seed, err := webResumeSeedFor(chatResumeFixtureID)
	if err != nil {
		t.Fatalf("webResumeSeedFor(%s): %v", chatResumeFixtureID, err)
	}
	if seed.Lock != nil {
		defer seed.Lock()
	}
	if len(seed.RecordedOptions) != 1 || seed.RecordedOptions[0] != webToolsFlag {
		t.Fatalf("RecordedOptions = %v, want [%s] — the record was not read back", seed.RecordedOptions, webToolsFlag)
	}
	toolsOn, _ := webApplyRecordedOptions(seed.RecordedOptions,
		func(string) bool { return false }, false, false)
	if !toolsOn {
		t.Fatalf("%s was not restored from a record that states it", webToolsFlag)
	}
}
