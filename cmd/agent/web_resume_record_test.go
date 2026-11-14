package main

// web_resume_record_test.go — the WRITE half of the web continuation, and the
// capability-loss warning that reports a restore which did not happen.
//
// THE MEASURED DEFECT (row 01a1b50c, 2026-11-07). The web face's READ side
// (webResumeSeedFor → webApplyRecordedOptions) was fully wired and always
// received an EMPTY slice, because nothing on this face ever wrote the record:
// `chatRecordResumeOptions` had exactly one caller, and it was the chat REPL
// (`cmd/agent/cli_chat.go`). So `gopi web --tools`, closed and continued as
// `gopi web <id>`, ran with no tools: the seeded history carried `tool_use`
// blocks, the request carried no tools array, and the model emitted its native
// tool-call syntax as plain text in the page — a capability lost with no
// sentence anywhere saying so (Rule 15).
//
// These tests drive the REAL seams in both directions: a live agent's own
// SessionLogger writes the record, and the resume resolver reads it back off
// that same file. Nothing here hand-builds the JSONL, because a fixture that
// both writes and reads its own shape would pass while the product's two halves
// disagreed — which is precisely the defect being pinned.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// webRecordFixtureAgent builds the agent a `gopi web` run really holds, in an
// isolated profile: NewAgent mints the session JSONL and the raw log inside
// GOPI_HOME, so the record this test writes lands on disk exactly where
// `gopi web <id>` looks for it afterwards.
func webRecordFixtureAgent(t *testing.T, gopiHome string) *agent.Agent {
	t.Helper()
	t.Setenv(config.GopiDirEnv, gopiHome)
	ag, err := agent.NewAgent("deepseek-v4-flash")
	if err != nil {
		t.Fatalf("agent.NewAgent: %v", err)
	}
	if ag.SessionLog == nil {
		t.Fatal("the agent has no SessionLog — the record could not be written by any face")
	}
	return ag
}

// TestWebRecordResumeOptionsRoundTripsThroughTheResumeReader is the headline:
// what the web face WRITES is what a later `gopi web <id>` READS.
//
// It is deliberately one test rather than two: the defect was the two halves
// disagreeing, and a test that asserted only the write would have passed on the
// broken code (the chat face's writer worked all along — nothing called it).
func TestWebRecordResumeOptionsRoundTripsThroughTheResumeReader(t *testing.T) {
	gopiHome, _ := chatResumeSandbox(t)
	ag := webRecordFixtureAgent(t, gopiHome)

	flags := webRecordedSwitches(true, true)
	if len(flags) != 2 {
		t.Fatalf("webRecordedSwitches(tools, thinking) = %v, want both switches", flags)
	}
	webRecordResumeOptions(ag, flags)

	// READ IT BACK THROUGH THE PRODUCT'S OWN READER, not through a decoder
	// written here.
	opts, found, err := chatReadRecordedOptions(ag.SessionLog.SessionID(), ag.RawLog.Path())
	if err != nil {
		t.Fatalf("chatReadRecordedOptions: %v", err)
	}
	if !found {
		t.Fatal("the web face wrote no resume record — a later `gopi web <id>` would restore nothing (this IS the defect)")
	}
	if strings.Join(opts.Flags, ",") != strings.Join(flags, ",") {
		t.Fatalf("recorded flags = %v, want %v", opts.Flags, flags)
	}
	if opts.SessionID != ag.SessionLog.SessionID() {
		t.Fatalf("recorded session id = %q, want %q — the record must name the run it belongs to",
			opts.SessionID, ag.SessionLog.SessionID())
	}
}

// TestWebRecordResumeOptionsWithBothSwitchesOffRecordsNothing: the absence is
// the statement (Rule 15). An event carrying an empty array would make "nothing
// to record" indistinguishable from "recorded an empty command line".
func TestWebRecordResumeOptionsWithBothSwitchesOffRecordsNothing(t *testing.T) {
	if got := webRecordedSwitches(false, false); len(got) != 0 {
		t.Fatalf("webRecordedSwitches(off, off) = %v, want nothing recorded", got)
	}
	gopiHome, _ := chatResumeSandbox(t)
	ag := webRecordFixtureAgent(t, gopiHome)
	webRecordResumeOptions(ag, webRecordedSwitches(false, false))

	_, found, err := chatReadRecordedOptions(ag.SessionLog.SessionID(), ag.RawLog.Path())
	if err != nil {
		t.Fatalf("chatReadRecordedOptions: %v", err)
	}
	if found {
		t.Fatal("a run with both switches off recorded an options event — 'nothing to record' became indistinguishable from a record")
	}
}

// TestWebRecordedSwitchesDerivesFromWhatTheRunGot: the record is the switches
// this run ACTUALLY RUNS WITH (after env defaults and after any restore), never
// a re-parse of argv — a resumed run must record the capability it was given,
// not the flags its operator happened not to type.
func TestWebRecordedSwitchesDerivesFromWhatTheRunGot(t *testing.T) {
	cases := []struct {
		name     string
		tools    bool
		thinking bool
		want     []string
	}{
		{"tools only", true, false, []string{webToolsFlag}},
		{"thinking only", false, true, []string{webThinkingFlag}},
		{"both", true, true, []string{webToolsFlag, webThinkingFlag}},
		{"neither", false, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := webRecordedSwitches(c.tools, c.thinking)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("webRecordedSwitches(%v, %v) = %v, want %v", c.tools, c.thinking, got, c.want)
			}
			// Every word recorded must be one THIS face parses back, or the
			// record would name a switch the reader cannot restore.
			for _, f := range got {
				if !webSwitchOfThisFace(f) {
					t.Fatalf("recorded %q, which this face does not apply", f)
				}
			}
		})
	}
}

// TestWebResumeSeedCarriesTheCheckpointsToolCount is the evidence the warning
// reports: a continued conversation that was generated WITH tools says so, so
// the loss can be named instead of merely happening.
func TestWebResumeSeedCarriesTheCheckpointsToolCount(t *testing.T) {
	gopiHome, _ := chatResumeSandbox(t)
	t.Setenv(config.GopiDirEnv, gopiHome)
	chatResumeFixtureWithTools(t, gopiHome, chatResumeFixtureID, 4)

	seed, err := webResumeSeedFor(chatResumeFixtureID)
	if err != nil {
		t.Fatalf("webResumeSeedFor(%s): %v", chatResumeFixtureID, err)
	}
	if seed.Lock != nil {
		defer seed.Lock()
	}
	if seed.ToolDefs != 4 {
		t.Fatalf("seed.ToolDefs = %d, want 4 — the warning cannot report a capability it never counted", seed.ToolDefs)
	}
}

// TestWebSeededToolsMismatchNamesTheWebRemedy: the warning must name a command
// line that ACTUALLY restores the capability on THIS face. The chat remedy
// (`--tools` alone) is wrong here — `gopi web --tools` starts a FRESH
// conversation, answering the loss with a different session.
func TestWebSeededToolsMismatchNamesTheWebRemedy(t *testing.T) {
	got := seededToolsMismatch(webResumeProgName, webResumeToolsRemedy, 18, false)
	if got == "" {
		t.Fatal("a tool-enabled checkpoint seeded into a run with tools OFF produced no warning — the capability is silently dropped")
	}
	if !strings.Contains(got, "18 tool definition(s)") {
		t.Errorf("the warning does not report what was carried:\n%s", got)
	}
	if !strings.Contains(got, webToolsFlag) {
		t.Errorf("the warning does not name %s — it states a problem without the remedy:\n%s", webToolsFlag, got)
	}
	if !strings.Contains(got, webResumeProgName) {
		t.Errorf("the warning does not name the face it is printed on:\n%s", got)
	}
	// THE REMEDY MUST BE A COMMAND LINE THIS FACE ACCEPTS: the address is the
	// positional, and it must survive this face's own STRICT parser.
	if _, err := webParseFlagsServer([]string{chatResumeFixtureID, webToolsFlag}, webDefaultAddrValue, false, false); err != nil {
		t.Fatalf("the remedy %q is not a command line this face accepts: %v", webResumeToolsRemedy, err)
	}
}

// TestWebSeededToolsMismatchStaysSilentWhenNothingIsLost: the two healthy
// shapes. A warning that fires on these is noise, and noise is how a real
// warning gets ignored.
func TestWebSeededToolsMismatchStaysSilentWhenNothingIsLost(t *testing.T) {
	cases := []struct {
		name     string
		toolDefs int
		toolsOn  bool
	}{
		{"tool-enabled checkpoint, tools ON", 18, true},
		{"plain checkpoint (no tools to lose)", 0, false},
		{"no checkpoint at all", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := seededToolsMismatch(webResumeProgName, webResumeToolsRemedy, c.toolDefs, c.toolsOn); got != "" {
				t.Errorf("warned when nothing was lost:\n%s", got)
			}
		})
	}
}

// TestWebApplyRecordedOptionsRestoresTheToolsTheRecordCarried is the end of the
// chain the defect broke: a record saying `--tools`, a command line that does
// not state it, and tools OFF must come back ON.
func TestWebApplyRecordedOptionsRestoresTheToolsTheRecordCarried(t *testing.T) {
	toolsOn, _ := webApplyRecordedOptions([]string{webToolsFlag}, func(string) bool { return false }, false, false)
	if !toolsOn {
		t.Fatal("a recorded --tools was not restored — a continued web session runs without the capability its record states")
	}
	// AND the warning must then be SILENT: the restore repaired the loss.
	if warn := seededToolsMismatch(webResumeProgName, webResumeToolsRemedy, 18, toolsOn); warn != "" {
		t.Fatalf("the warning fired after the restore repaired the loss (the two sentences would contradict each other):\n%s", warn)
	}
}

// TestWebServerParserRefusesAnUnknownFlag — the SECOND silent failure in this
// path (Rule 15). The shared parser ignored every word it did not know, so a
// misspelt--tool was accepted in silence and the capability was dropped with no
// sentence naming why. The SERVER face (`gopi web`) owns its whole vocabulary,
// so it refuses; the other two faces that share this parser (`gopi cli`,
// `gopi cli --once`) own further switches and keep the permissive reading.
func TestWebServerParserRefusesAnUnknownFlag(t *testing.T) {
	if _, err := webParseFlagsServer([]string{"--tool"}, webDefaultAddrValue, false, false); err == nil {
		t.Fatal("`gopi web --tool` was accepted in silence — a misspelt switch must be refused, not ignored")
	} else if !strings.Contains(err.Error(), "--tool") {
		t.Errorf("the refusal does not name the word it refused: %v", err)
	}
	// THE SHARED PARSER STAYS PERMISSIVE: `gopi cli --once --prompt x` relies on
	// this function not judging words another face owns.
	if _, err := webParseFlags([]string{"--once", "--prompt", "x"}, webDefaultAddrValue, false, false); err != nil {
		t.Fatalf("the shared parser refused a word another face owns (%v) — one face's vocabulary would become every face's", err)
	}
	// A LONE "-" IS AN OPERAND, not an unknown flag.
	if _, err := webParseFlagsServer([]string{"-"}, webDefaultAddrValue, false, false); err != nil {
		t.Errorf("a lone \"-\" was refused as an unknown flag: %v", err)
	}
	// AND THE ADDRESS ITSELF IS STILL ACCEPTED beside a known switch.
	opts, err := webParseFlagsServer([]string{chatResumeFixtureID, webToolsFlag}, webDefaultAddrValue, false, false)
	if err != nil {
		t.Fatalf("the server parser refused its own address form: %v", err)
	}
	if len(opts.Positional) != 1 || !opts.Tools {
		t.Fatalf("opts = %+v, want the address collected and --tools applied", opts)
	}
}

// chatResumeFixtureWithTools builds the same session fixture the chat tests use,
// with a CHECKPOINT THAT CARRIED TOOL DEFINITIONS — the evidence a continuation
// was a tool-enabled run. It is separate from chatResumeFixture because that
// one's checkpoint is deliberately plain (a run with no tools), and the two
// shapes must stay distinguishable.
func chatResumeFixtureWithTools(t *testing.T, gopiHome, sessionID string, toolDefs int) string {
	t.Helper()
	dir := chatResumeSessionDir(gopiHome, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	tools := `[`
	for i := 0; i < toolDefs; i++ {
		if i > 0 {
			tools += `,`
		}
		tools += `{"name":"tool-` + string(rune('a'+i)) + `","description":"a tool the run had","input_schema":{"type":"object"}}`
	}
	tools += `]`
	ckpt := `{"model":"deepseek-v4-flash","stream":true,"system":[{"type":"text","text":"sys"}],` +
		`"messages":[{"role":"user","content":"the tool-enabled run's question"}],"tools":` + tools + `,"thinking":null}`
	raw := filepath.Join(dir, sessionID+".raw.log")
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"+ckpt+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	line := `{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"` + sessionID +
		`","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-11-07T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	return raw
}
