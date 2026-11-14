package main

// runtime_config_snapshot_test.go — the regression guards for `/config`.
//
// THREE THINGS THIS FILE PINS, and each is a claim the feature would be
// worthless without:
//
//  1. THE FIVE CHANNELS are decided by ONE classifier, driven directly here —
//     flag / env / settings.json / NOT APPLIED / (unset). `NOT APPLIED` is the
//     soul of the feature (the document states a value, the process never
//     received it), so it is asserted from the classifier AND from a rendered
//     block, never from a comment.
//  2. THE ROWS ARE READ FROM THE PROCESS, through the dependency seam: the
//     fixture document and the fixture environment are put in front of ONE
//     builder, so a passing test cannot be a test that happened to read the
//     operator's own profile.
//  3. CREDENTIALS ARE REDACTED by NAME, and the thinking coupling is stated as a
//     `!` note — the two places a report of this kind leaks or misleads.
//
// THE FIXTURES ARE SYNTHETIC ON PURPOSE. Nothing here sets GOPI_* on the real
// process: the deps struct exists so the environment is a map, not the operator's.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/thinkmode"
)

// runtimeConfigTestDeps builds a dependency set from two literal maps: the
// environment this "process" carries, and the document it resolved from. It is
// the whole seam — the builder cannot reach anything else.
func runtimeConfigTestDeps(env, document map[string]string, src runtimeConfigEnvSources) runtimeConfigDeps {
	return runtimeConfigDeps{
		Getenv: func(name string) string { return env[name] },
		Document: func(key string) string {
			return document[key]
		},
		Sources: src,
	}
}

// rowFor returns the rendered line whose key column is key, or "" when the
// report carries no such row.
func rowFor(lines []string, key string) string {
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == key {
			return line
		}
	}
	return ""
}

// TestRuntimeConfigChannelOfClassifiesEveryChannel is the classifier's own
// guard: the five outcomes, each from the facts that decide it, so a change that
// collapses two channels into one is a red test rather than a subtler report.
func TestRuntimeConfigChannelOfClassifiesEveryChannel(t *testing.T) {
	withFlag := runtimeConfigEnvSources{Flag: map[string]bool{"GOPI_THINKING": true}}
	withSettings := runtimeConfigEnvSources{Settings: map[string]bool{"GOPI_THINKING": true}}

	cases := []struct {
		name     string
		envName  string
		inForce  string
		document string
		src      runtimeConfigEnvSources
		want     string
	}{
		{"a flag decided it", "GOPI_THINKING", "enabled", "disabled", withFlag, runtimeConfigChannelFlag},
		{"the document was published", "GOPI_THINKING", "enabled", "enabled", withSettings, runtimeConfigChannelSettings},
		{"an operator export", "GOPI_THINKING", "enabled", "", runtimeConfigEnvSources{}, runtimeConfigChannelEnv},
		{"THE row: document states it, process does not hold it", "GOPI_REASONING_EFFORT", "", "high", runtimeConfigEnvSources{}, runtimeConfigChannelNotApplied},
		{"nothing specified it", "GOPI_REASONING_EFFORT", "", "", runtimeConfigEnvSources{}, runtimeConfigChannelUnset},
		{"nothing specified it and there is no default", "GOPI_KEEP_THINKING", "", "", runtimeConfigEnvSources{}, runtimeConfigChannelUnset},
		{"a flag beats the document's own record", "GOPI_THINKING", "enabled", "enabled",
			runtimeConfigEnvSources{Flag: map[string]bool{"GOPI_THINKING": true}, Settings: map[string]bool{"GOPI_THINKING": true}},
			runtimeConfigChannelFlag},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runtimeConfigChannelOf(c.envName, c.inForce, c.document, c.src); got != c.want {
				t.Errorf("channel = %q, want %q (env=%q document=%q)", got, c.want, c.inForce, c.document)
			}
		})
	}
}

// TestRuntimeConfigSnapshotMarksNotApplied is deliverable ②/③ as a RENDERED
// assertion: the acceptance criterion names the exact row shape
// `reasoning_effort (unset) [NOT APPLIED]`, so the block must carry a row whose
// key is the knob, whose value column says the process holds nothing, and whose
// channel column says the document stated it anyway.
func TestRuntimeConfigSnapshotMarksNotApplied(t *testing.T) {
	deps := runtimeConfigTestDeps(
		map[string]string{}, // the process holds NOTHING — the 2026-11-12 shape
		map[string]string{
			settingsKeyReasoningEffort: "high",
			settingsKeyThinkingBudget:  "16384",
		},
		runtimeConfigEnvSources{},
	)
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil, deps))
	if len(lines) == 0 || lines[0] != runtimeConfigTitle {
		t.Fatalf("first line is not the title %q:\n%s", runtimeConfigTitle, strings.Join(lines, "\n"))
	}

	row := rowFor(lines, settingsKeyReasoningEffort)
	if row == "" {
		t.Fatalf("no row for %s:\n%s", settingsKeyReasoningEffort, strings.Join(lines, "\n"))
	}
	for _, want := range []string{runtimeConfigChannelUnset, runtimeConfigChannelNotApplied} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q does not carry %q — the acceptance criterion names this exact shape", row, want)
		}
	}

	// The same fact in WORDS below the table: a reader scanning the `!` lines
	// must learn that the document's value is dead, not merely that a column
	// says so.
	body := strings.Join(lines, "\n")
	// The knobs NOTHING specified name the code's own default IN THE VALUE
	// CELL, because either fact alone is misleading: `(unset)` alone leaves a
	// reader unable to tell 16384 from 0, and a bare `16384` would claim the
	// process holds something it does not. The row states both.
	rows := map[string]string{}
	for _, line := range lines {
		if f := strings.Fields(line); len(f) > 0 {
			rows[f[0]] = line
		}
	}
	wantDefault := runtimeConfigBuiltinDefaultOf(settingsKeyMaxTokens)
	if wantDefault == "" {
		t.Fatalf("%s has no compiled-in default any more — this test's premise is gone", settingsKeyMaxTokens)
	}
	if row := rows[settingsKeyMaxTokens]; !strings.Contains(row, runtimeConfigChannelDefault) ||
		!strings.Contains(row, runtimeConfigChannelUnset) || !strings.Contains(row, wantDefault) {
		t.Errorf("the %s row does not state BOTH that nothing is set AND which value runs (%s): %q",
			settingsKeyMaxTokens, wantDefault, row)
	}
	if row := rows[settingsKeyKeepThinking]; !strings.Contains(row, "["+runtimeConfigChannelUnset+"]") {
		t.Errorf("a knob with no default was not reported unset: %q", row)
	}

	if !strings.Contains(body, runtimeConfigNoteMarker) {
		t.Errorf("no %q note line below the table:\n%s", strings.TrimSpace(runtimeConfigNoteMarker), body)
	}
	if !strings.Contains(body, "reasoning_effort is stated by the settings document but NOT held by this process") {
		t.Errorf("the NOT APPLIED note does not name the knob:\n%s", body)
	}
}

// TestRuntimeConfigSnapshotNamesTheRunningDefaultInTheValueCell is THE
// regression guard for the 2026-11-13 defect (bug row 01a1d15d-9b5f): the
// report's value and channel columns used to CONTRADICT each other and the row
// below.
//
// THE MEASURED SHAPE BEFORE THE FIX:
//
//	max_tokens      (unset)  [built-in default]
//	…
//	agent.max_tokens  16384  [agent]
//
// A reader could not tell "16384 will be sent" from "nothing is set", and the
// two rows looked like they disagreed about one fact. Both columns were
// individually true and jointly useless.
//
// TWO CLAIMS ARE PINNED, and the second is the one a naive fix drops:
//
//  1. A knob with NO channel and a compiled-in default states BOTH halves in
//     the value cell — nothing is held, and THIS is what runs — with the
//     default read from runtimeConfigBuiltinDefaultOf, never a literal here.
//  2. A knob with NO compiled-in default is NOT labelled `[built-in default]`.
//     That label is a promise that the code has a fallback; on `keep_thinking`
//     there is none, and the label would be a fiction.
func TestRuntimeConfigSnapshotNamesTheRunningDefaultInTheValueCell(t *testing.T) {
	// A synthetic agent whose resolved fields are the code's own defaults, so
	// the third claim below compares the report against ITSELF rather than
	// against a number this test typed.
	ag := &agent.Agent{
		MaxTokens:     deepseek.DefaultClaudeMaxTokens,
		ContextWindow: agent.DefaultContextWindow,
	}
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag,
		runtimeConfigTestDeps(map[string]string{}, map[string]string{}, runtimeConfigEnvSources{})))

	// (1) Every knob that HAS a default and nothing specified names that value —
	// INCLUDING `thinking`, and only because THIS fixture's environment carries no
	// reasoning_effort: with none in force the knob is not coupled, and the
	// compiled-in default really is what runs. The coupled case is the opposite
	// claim and has its own guard (TestRuntimeConfigCoupledThinkingAgreesWith-
	// TheResolvedRow, bug row 01a1d16d-9ce7): there the value column must NOT name
	// the default, because another row decides it. Asserting the default here
	// while the environment says nothing is what makes that second test meaningful
	// — the claim is conditional on the coupling, not deleted with it.
	if effort := runtimeConfigTestDeps(map[string]string{}, map[string]string{}, runtimeConfigEnvSources{}).Getenv(agent.ReasoningEffortEnvName); effort != "" {
		t.Fatalf("this fixture's environment carries %s=%q — the uncoupled claim below would then be wrong", agent.ReasoningEffortEnvName, effort)
	}
	// `effort_values` belongs in THIS list, not in the no-default list below: the
	// resolver really does fall back to a baseline whitelist (bug row 01a1d207),
	// so the row must name it. It sat in the no-default group until this fix,
	// which is exactly what made the row answer nothing.
	for _, key := range []string{
		settingsKeyMaxTokens,
		settingsKeyContextWindow,
		settingsKeyThinking,
		settingsKeyThinkingBudget,
		settingsKeyEffortValues,
		settingsKeyTimeout,
	} {
		row := rowFor(lines, key)
		if row == "" {
			t.Fatalf("no %q row:\n%s", key, strings.Join(lines, "\n"))
		}
		want := runtimeConfigBuiltinDefaultOf(key)
		if want == "" {
			t.Fatalf("%s no longer has a compiled-in default — this test's premise is gone", key)
		}
		if !strings.Contains(row, "["+runtimeConfigChannelDefault+"]") {
			t.Errorf("row %q is not labelled %q", row, runtimeConfigChannelDefault)
		}
		if !strings.Contains(row, want) {
			t.Errorf("row %q does not name the value that runs (%q) — the value column still answers half the question", row, want)
		}
		if !strings.Contains(row, runtimeConfigChannelUnset) {
			t.Errorf("row %q does not say the process holds nothing (%q)", row, runtimeConfigChannelUnset)
		}
	}

	// (2) A knob with NO default is never labelled `[built-in default]`, and its
	// value cell stays the plain `(unset)`.
	for _, key := range []string{settingsKeyKeepThinking, settingsKeyHideThinking} {
		row := rowFor(lines, key)
		if row == "" {
			t.Fatalf("no %q row:\n%s", key, strings.Join(lines, "\n"))
		}
		if runtimeConfigBuiltinDefaultOf(key) != "" {
			t.Fatalf("%s gained a compiled-in default — this test's premise is gone", key)
		}
		if strings.Contains(row, runtimeConfigChannelDefault) {
			t.Errorf("row %q is labelled %q but the code has no default for it — the label promises a fallback that does not exist",
				row, runtimeConfigChannelDefault)
		}
		if !strings.Contains(row, "["+runtimeConfigChannelUnset+"]") {
			t.Errorf("a knob with no default was not reported unset: %q", row)
		}
	}

	// (3) The surface contradiction itself is gone: what the unresolved row says
	// will run IS what the resolved row prints. Two rows, one fact.
	for _, pair := range [][2]string{
		{settingsKeyMaxTokens, "agent.max_tokens"},
		{settingsKeyContextWindow, "agent.context_window"},
	} {
		knobRow, resolvedRow := rowFor(lines, pair[0]), rowFor(lines, pair[1])
		if knobRow == "" || resolvedRow == "" {
			t.Fatalf("missing row (%q / %q):\n%s", pair[0], pair[1], strings.Join(lines, "\n"))
		}
		resolved := strings.Fields(resolvedRow)
		if len(resolved) < 2 {
			t.Fatalf("resolved row %q has no value column", resolvedRow)
		}
		if !strings.Contains(knobRow, resolved[1]) {
			t.Errorf("the %s row does not name the value the resolved %s row prints (%q): %q",
				pair[0], pair[1], resolved[1], knobRow)
		}
	}
}

// TestRuntimeConfigSnapshotReadsTheProcessNotTheFile pins the differential the
// whole feature rests on: the SAME document with two different environments must
// produce two different reports. A report that could not tell those apart would
// be `gopi config show`'s duplicate — the exact thing this surface exists not to
// be.
func TestRuntimeConfigSnapshotReadsTheProcessNotTheFile(t *testing.T) {
	document := map[string]string{settingsKeyMaxTokens: "16384", settingsKeyModel: "from-the-file"}

	dead := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil,
		runtimeConfigTestDeps(map[string]string{}, document, runtimeConfigEnvSources{})))
	live := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil,
		runtimeConfigTestDeps(map[string]string{"GOPI_MAX_TOKENS": "4096"}, document,
			runtimeConfigEnvSources{Settings: map[string]bool{"GOPI_MAX_TOKENS": true}})))

	if deadRow, liveRow := rowFor(dead, settingsKeyMaxTokens), rowFor(live, settingsKeyMaxTokens); deadRow == liveRow {
		t.Fatalf("the max_tokens row is identical whether or not the process holds the value (%q) — the report is reading the FILE, not the process", deadRow)
	}
	if !strings.Contains(rowFor(live, settingsKeyMaxTokens), "4096") {
		t.Errorf("the live row does not carry the process's own value:\n%s", rowFor(live, settingsKeyMaxTokens))
	}
	if !strings.Contains(rowFor(live, settingsKeyMaxTokens), runtimeConfigChannelSettings) {
		t.Errorf("a value this run published from the document is not labelled %q:\n%s",
			runtimeConfigChannelSettings, rowFor(live, settingsKeyMaxTokens))
	}
}

// TestRuntimeConfigRedactsCredentialsByShape pins the redaction: a credential's
// VALUE never reaches the rendered block, whatever it looks like, because the
// NAME decides. A `<set>` marker that only fired on a non-empty value would leak
// the moment a key was short.
func TestRuntimeConfigRedactsCredentialsByShape(t *testing.T) {
	const secret = "sk-do-not-print-me"
	deps := runtimeConfigTestDeps(
		map[string]string{"ANTHROPIC_AUTH_TOKEN": secret},
		map[string]string{settingsKeyAPIKey: secret},
		runtimeConfigEnvSources{},
	)
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil, deps))
	body := strings.Join(lines, "\n")
	if strings.Contains(body, secret) {
		t.Fatalf("the credential leaked into the report:\n%s", body)
	}
	row := rowFor(lines, settingsKeyAPIKey)
	if !strings.Contains(row, runtimeConfigSeedRedacted) {
		t.Errorf("the credential row does not render %q: %q", runtimeConfigSeedRedacted, row)
	}
	// A non-credential row is NOT redacted — otherwise "everything is <set>"
	// would pass this test while telling the operator nothing.
	plain := redactIfSecret(settingsKeyModel, "deepseek-v4-flash")
	if plain != "deepseek-v4-flash" {
		t.Errorf("a non-credential value was redacted: %q", plain)
	}
}

// TestRuntimeConfigStatesTheThinkingCoupling is the `!` note's guard: when the
// agent will send thinking=disabled, an effort that is SET is a value that never
// reaches the wire — the coupling a reader cannot derive from the values.
func TestRuntimeConfigStatesTheThinkingCoupling(t *testing.T) {
	ag := &agent.Agent{
		Model:           "fixture-model",
		ReasoningEffort: "high",
		Thinking:        &deepseek.Thinking{Type: thinkmode.Disabled},
		MaxTokens:       deepseek.DefaultClaudeMaxTokens,
		ContextWindow:   agent.DefaultContextWindow,
	}
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag, runtimeConfigTestDeps(
		map[string]string{"GOPI_REASONING_EFFORT": "high"}, map[string]string{}, runtimeConfigEnvSources{})))
	body := strings.Join(lines, "\n")
	if !strings.Contains(body, runtimeConfigNoteMarker+"thinking is OFF while reasoning_effort=") {
		t.Fatalf("the thinking/effort coupling is not stated as a note:\n%s", body)
	}
	// The RESOLVED rows are present too — the agent's own answer, distinct from
	// the environment's.
	for _, key := range []string{"agent.model", "agent.thinking", "agent.max_tokens", "agent.context_window"} {
		if rowFor(lines, key) == "" {
			t.Errorf("no resolved row for %s:\n%s", key, body)
		}
	}
	if !strings.Contains(rowFor(lines, "agent.model"), "fixture-model") {
		t.Errorf("the resolved model row does not carry the agent's own model: %q", rowFor(lines, "agent.model"))
	}
}

// TestRuntimeConfigTmpDirRowFollowsItsPublishedLabel pins the temp-root row to
// the PAIR the publisher wrote (TMPDIR + GOPI_TMPDIR_SOURCE) rather than to a
// second ranking of its channels.
//
// THE MEASURED DEFECT THIS CLOSES: the first real run of this command printed
// `tmpdir … NOT APPLIED` on a host where the temp root was set and in force,
// because only the INPUT name (GOPI_TMPDIR) was read and the OUTPUT name (TMPDIR)
// was not. A false alarm in this column would destroy the column's only value.
func TestRuntimeConfigTmpDirRowFollowsItsPublishedLabel(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		source    string
		document  string
		wantRow   string
		wantChans []string
	}{
		{"published from the settings key", "/w/_tmp", config.TmpDirSourceSettings, "/w/_tmp", "/w/_tmp", []string{runtimeConfigChannelSettings}},
		{"the caller's own export", "/ambient", config.TmpDirSourceAmbient, "", "/ambient", []string{runtimeConfigChannelEnv}},
		{"published from gopi's own env name", "/fromgopi", config.TmpDirSourceEnv, "", "/fromgopi", []string{runtimeConfigChannelEnv}},
		{"published from the flag", "/fromflag", config.TmpDirSourceFlag, "", "/fromflag", []string{runtimeConfigChannelFlag}},
		{"a label nobody recognises is reported verbatim", "/odd", "from some future channel", "", "/odd", []string{"from some future channel"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := map[string]string{scratch.TmpDirEnvName: c.value}
			if c.source != "" {
				env[scratch.TmpDirSourceEnvName] = c.source
			}
			lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil,
				runtimeConfigTestDeps(env, map[string]string{settingsKeyTmpDir: c.document}, runtimeConfigEnvSources{})))
			row := rowFor(lines, settingsKeyTmpDir)
			if row == "" {
				t.Fatalf("no %s row:\n%s", settingsKeyTmpDir, strings.Join(lines, "\n"))
			}
			if !strings.Contains(row, c.wantRow) {
				t.Errorf("row %q does not carry the published value %q", row, c.wantRow)
			}
			for _, want := range c.wantChans {
				if !strings.Contains(row, want) {
					t.Errorf("row %q does not carry the channel %q", row, want)
				}
			}
			if strings.Contains(row, runtimeConfigChannelNotApplied) {
				t.Errorf("a temp root the process HOLDS was reported NOT APPLIED: %q", row)
			}
		})
	}
}

// TestRuntimeConfigTmpDirNotAppliedNeedsAnEmptyProcess completes the pair: with
// NO published value and NO label, a document that states one IS the
// "document has it, process does not" case — the row must say so.
func TestRuntimeConfigTmpDirNotAppliedNeedsAnEmptyProcess(t *testing.T) {
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil,
		runtimeConfigTestDeps(map[string]string{}, map[string]string{settingsKeyTmpDir: "/doc/_tmp"}, runtimeConfigEnvSources{})))
	row := rowFor(lines, settingsKeyTmpDir)
	if !strings.Contains(row, runtimeConfigChannelNotApplied) {
		t.Errorf("a stated temp root that never reached the process is not marked %q: %q", runtimeConfigChannelNotApplied, row)
	}
}

// TestRuntimeConfigHelpAnswersBothDoors pins the HARD help standard for this
// word: `/help config`, `/config help` and the table's `-h` all answer the ONE
// usage block, so a reader who learned the spelling on one carrier has learned
// it on both.
func TestRuntimeConfigHelpAnswersBothDoors(t *testing.T) {
	if _, ok := slashCommands[chatConfigCommandName]; !ok {
		t.Fatalf("/%s is missing from the shared table", chatConfigCommandName)
	}
	viaHelp, err := chatHelpText("/help " + chatConfigCommandName)
	if err != nil {
		t.Fatalf("chatHelpText(/help %s): %v", chatConfigCommandName, err)
	}
	if viaHelp != chatConfigUsage {
		t.Errorf("/help %s differs from the usage block:\n--- via /help ---\n%s\n--- Usage ---\n%s", chatConfigCommandName, viaHelp, chatConfigUsage)
	}
	for _, form := range []string{chatConfigHelpWord, chatConfigHelpFlagShort, chatConfigHelpFlagLong} {
		if !chatConfigWantsHelp(form) {
			t.Errorf("/%s %s does not resolve to the usage block", chatConfigCommandName, form)
		}
		if out, err := execSlashCommand("/"+chatConfigCommandName+" "+form, CommandContext{}); err != nil || out != chatConfigUsage {
			t.Errorf("/%s %s = %q, %v; want the usage block", chatConfigCommandName, form, out, err)
		}
	}
	if chatConfigWantsHelp("") {
		t.Error("a bare /config was read as a help request — the report would be unreachable")
	}
	if !strings.Contains(chatConfigUsage, "Usage: /"+chatConfigCommandName) {
		t.Errorf("the usage block's first line is not the Usage line:\n%s", chatConfigUsage)
	}
}

// TestRuntimeConfigHelpListsTheWordExactlyOnce is 73a404540's guard, applied to
// this word: the shared table is the ONLY owner of a `/help` row, so the REPL's
// hand-maintained block must not list it too. Counted over the RENDERED page by
// row shape, so a sentence that mentions the word is not miscounted.
func TestRuntimeConfigHelpListsTheWordExactlyOnce(t *testing.T) {
	page := chatUsage + slashHelpText()
	rows := 0
	for _, line := range strings.Split(page, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == chatSlashTrigger+chatConfigCommandName {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("/help carries a row for %s%d %d time(s), want exactly 1 — the table is the only owner of a help row:\n%s",
			chatSlashTrigger, rows, rows, page)
	}
}

// TestRuntimeConfigArgIsStrippedOnce pins the argument reader the REPL switch
// and the help answer share, so `/config help`, `/config  help` and `/config -h`
// cannot disagree about where the argument begins.
func TestRuntimeConfigArgIsStrippedOnce(t *testing.T) {
	cases := map[string]string{
		"/config":       "",
		"/config help":  chatConfigHelpWord,
		"/config  help": chatConfigHelpWord,
		"/config -h":    chatConfigHelpFlagShort,
	}
	for line, want := range cases {
		if got := chatConfigArg(line); got != want {
			t.Errorf("chatConfigArg(%q) = %q, want %q", line, got, want)
		}
	}
}

// TestRuntimeConfigResolutionIsRecordedFromObservation pins the differential
// recorder: a value the settings load CHANGED is the document's, a value the
// process already carried is the operator's own. The distinction is measured by
// the recorder and read back through the ONE snapshot accessor.
//
// It restores the process record afterwards, so a sibling test cannot inherit
// this fixture's channel labels.
func TestRuntimeConfigResolutionIsRecordedFromObservation(t *testing.T) {
	prev := runtimeConfigSourcesSnapshot()
	t.Cleanup(func() { recordEnvSources(prev.Flag, prev.Settings) })

	before := map[string]string{"GOPI_THINKING": "enabled", "GOPI_MAX_TOKENS": "4096"}
	// The document supplies max_tokens; nothing touches thinking. Neither is
	// present in `after` unless something published it, so the recorder's answer
	// is read from the DELTA, not from a second document read.
	t.Setenv("GOPI_MAX_TOKENS", "16384")
	t.Setenv("GOPI_THINKING", "enabled")
	recordResolutionSources(before, nil)

	src := runtimeConfigSourcesSnapshot()
	if !src.Settings["GOPI_MAX_TOKENS"] {
		t.Error("a value the resolution CHANGED was not attributed to the settings document")
	}
	if src.Settings["GOPI_THINKING"] {
		t.Error("an inherited value was attributed to the settings document — the operator's own export would be mislabelled")
	}
}

// TestRuntimeConfigCoupledThinkingAgreesWithTheResolvedRow is THE regression
// guard for the 2026-11-13 defect (bug row 01a1d16d-9ce7), and it drives the
// REAL coupling rather than a string shape.
//
// THE MEASURED CONTRADICTION, in ONE render of ONE process:
//
//	thinking          (unset), disabled runs  [built-in default]
//	…
//	agent.thinking    enabled                 [agent]
//
// Both lines were true about different questions and jointly false about the
// one a reader asks: internal/agent's resolution switch auto-enables thinking
// when an explicit, valid, non-"off" reasoning_effort is set with no explicit
// thinking (agent.EffortImpliesThinking). So the compiled-in default was NOT what
// ran, and the value column — which must only ever state what the environment,
// the document or the constant HOLDS — had claimed a resolved fact.
//
// WHY IT STARTS FROM A REAL agent.NewAgent: the coupling is decided inside the
// resolver, so a fixture struct typed here would assert this test's own reading
// of the switch rather than the switch. The agent below is built from an
// environment that carries reasoning_effort=high and NO GOPI_THINKING — the exact
// production shape measured on this host — and BOTH halves of the report are then
// read from it.
func TestRuntimeConfigCoupledThinkingAgreesWithTheResolvedRow(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-key")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("GOPI_EFFORT_VALUES", "")
	t.Setenv(agent.ThinkingEnvName, "")            // NOT set: the coupling's precondition
	t.Setenv(agent.ReasoningEffortEnvName, "high") // explicit, valid, non-off
	testutil.NewTestDBPath(t)

	ag, err := agent.NewAgent("deepseek-v4-flash")
	if err != nil {
		t.Fatalf("agent.NewAgent: %v", err)
	}
	if runtimeConfigThinkingDisabled(ag) {
		t.Fatalf("premise gone: reasoning_effort=high with no GOPI_THINKING did not auto-enable thinking (agent.thinking=%s) — the coupling this test guards no longer exists in the resolver",
			runtimeConfigThinkingOf(ag.Thinking))
	}

	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag, liveRuntimeConfigDeps(runtimeConfigKnownKeys)))
	body := strings.Join(lines, "\n")

	knob := rowFor(lines, settingsKeyThinking)
	if knob == "" {
		t.Fatalf("no %q row:\n%s", settingsKeyThinking, body)
	}
	// (1) The value column must NOT claim the compiled-in default runs. This is
	// the defect verbatim: `(unset), disabled runs`.
	if strings.Contains(knob, runtimeConfigBuiltinDefaultOf(settingsKeyThinking)+" runs") {
		t.Errorf("the %s row still claims the compiled-in default runs: %q", settingsKeyThinking, knob)
	}
	// (2) Nor may the channel column name a default: that default does not run,
	// so labelling the row `[built-in default]` would be the same false claim one
	// column over.
	if strings.Contains(knob, "["+runtimeConfigChannelDefault+"]") {
		t.Errorf("the %s row is labelled %q while another row decides it: %q", settingsKeyThinking, runtimeConfigChannelDefault, knob)
	}
	// (3) The channel instead NAMES the row that decides it — an answer, not a
	// silence.
	if want := "[" + runtimeConfigDecidedByChannel(settingsKeyReasoningEffort) + "]"; !strings.Contains(knob, want) {
		t.Errorf("the %s row does not name the row that decides it (%s): %q", settingsKeyThinking, want, knob)
	}
	// (4) The two halves of the report agree: what the note says the request
	// carries IS what the resolved row prints.
	resolved := runtimeConfigThinkingOf(ag.Thinking)
	if resolvedRow := rowFor(lines, "agent.thinking"); !strings.Contains(resolvedRow, resolved) {
		t.Errorf("agent.thinking row %q does not carry the agent's own resolution %q", resolvedRow, resolved)
	}
	if !strings.Contains(body, runtimeConfigNoteMarker+"thinking is decided by reasoning_effort=") {
		t.Fatalf("the coupling is not stated as a %q note:\n%s", strings.TrimSpace(runtimeConfigNoteMarker), body)
	}
	if !strings.Contains(body, "so the request carries thinking="+resolved) {
		t.Errorf("the coupling note does not state what the request actually carries (%s):\n%s", resolved, body)
	}
}

// TestRuntimeConfigUncoupledThinkingStillNamesItsDefault is the CONTROL: with no
// reasoning_effort in force, `thinking` is NOT coupled, and the row must keep the
// `(unset), <default> runs [built-in default]` shape. Without this, deleting the
// claim everywhere would pass the regression test above while destroying the
// feature it belongs to (a value column that never names what runs).
func TestRuntimeConfigUncoupledThinkingStillNamesItsDefault(t *testing.T) {
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil,
		runtimeConfigTestDeps(map[string]string{}, map[string]string{}, runtimeConfigEnvSources{})))
	knob := rowFor(lines, settingsKeyThinking)
	if knob == "" {
		t.Fatalf("no %q row:\n%s", settingsKeyThinking, strings.Join(lines, "\n"))
	}
	want := runtimeConfigBuiltinDefaultOf(settingsKeyThinking)
	if want == "" {
		t.Fatalf("%s has no compiled-in default — this test's premise is gone", settingsKeyThinking)
	}
	if !strings.Contains(knob, want+" runs") || !strings.Contains(knob, "["+runtimeConfigChannelDefault+"]") {
		t.Errorf("an UNCOUPLED %s row no longer names the value that runs (%q): %q", settingsKeyThinking, want, knob)
	}
	if strings.Contains(knob, runtimeConfigChannelDecidedByPrefix) {
		t.Errorf("an uncoupled %s row claims another row decides it: %q", settingsKeyThinking, knob)
	}
}

// TestRuntimeConfigEffortOffDoesNotImplyThinking pins the coupling's own escape:
// the one effort value that switches thinking OFF must not be read as "effort is
// set, therefore thinking is on". It is the boundary a naive "is effort set?"
// predicate gets wrong, in the opposite direction of the bug this file guards.
func TestRuntimeConfigEffortOffDoesNotImplyThinking(t *testing.T) {
	off := func(name string) string {
		switch name {
		case agent.ReasoningEffortEnvName:
			return config.EffortOff
		}
		return ""
	}
	if runtimeConfigDecidedByOf(nil, settingsKeyThinking, "", "", off) != "" {
		t.Errorf("reasoning_effort=%q was read as deciding thinking ON — that is the value that disables it", config.EffortOff)
	}
	// And a value the provider's own schema rejects does not imply intent either:
	// the resolver falls back to the baseline, so nothing was asked for.
	bogus := func(name string) string {
		switch name {
		case agent.ReasoningEffortEnvName:
			return "definitely-not-an-effort"
		}
		return ""
	}
	if runtimeConfigDecidedByOf(nil, settingsKeyThinking, "", "", bogus) != "" {
		t.Error("an invalid effort was read as implying thinking — the resolver's ruling is that an invalid value does not imply intent")
	}
	// An explicit thinking setting owns the switch, so the effort cannot decide.
	explicit := func(name string) string {
		switch name {
		case agent.ReasoningEffortEnvName:
			return "high"
		case agent.ThinkingEnvName:
			return thinkmode.Enabled
		}
		return ""
	}
	if runtimeConfigDecidedByOf(nil, settingsKeyThinking, "", "", explicit) != "" {
		t.Error("an explicit GOPI_THINKING was overruled by the effort in the report — the explicit setting wins in the resolver")
	}
}

// TestRuntimeConfigEffortValuesNamesTheBaselineWhitelist is THE regression guard
// for the 2026-11-13 defect (bug row 01a1d207-1b98): the `effort_values` row
// printed `(unset)` in BOTH columns while `internal/config` really does fall back
// to a baseline whitelist (minimal|low|medium|high|max) inside
// resolveReasoningEffort. The row's whole purpose is answering "is `high` a legal
// effort value?", and it answered "nothing is known" instead.
//
// THE ROOT CAUSE was one missing case: runtimeConfigBuiltinDefaultOf had no
// settingsKeyEffortValues arm, so it returned "" and the row was classified as a
// knob with no default. TWO CLAIMS ARE PINNED, one per state of the knob:
//
//  1. NOTHING DECLARED (empty environment, empty document) — the row names the
//     baseline list and says `[built-in default]`, the same shape max_tokens and
//     context_window render. The list is read from the row, never retyped here,
//     and the two members named are the ones a reader actually asks about
//     (`minimal` is the resolver's own fallback, `high` is what this host runs).
//  2. THE DOCUMENT DECLARED A LIST — the row prints that list and nothing about a
//     default, and its channel is settings.json. This is the state the fix must
//     not damage: a report that named the baseline over a declared list would be
//     the same defect pointing the other way.
func TestRuntimeConfigEffortValuesNamesTheBaselineWhitelist(t *testing.T) {
	// (1) NOTHING DECLARED.
	empty := runtimeConfigTestDeps(map[string]string{}, map[string]string{}, runtimeConfigEnvSources{})
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil, empty))
	row := rowFor(lines, settingsKeyEffortValues)
	if row == "" {
		t.Fatalf("no %q row:\n%s", settingsKeyEffortValues, strings.Join(lines, "\n"))
	}
	wantDefault := runtimeConfigBuiltinDefaultOf(settingsKeyEffortValues)
	if wantDefault == "" {
		t.Fatalf("%s reports no compiled-in default — the resolver's baseline whitelist is no longer visible to the report, which IS the defect this test guards",
			settingsKeyEffortValues)
	}
	// The value column NAMES the list the resolver falls back to. Read from the
	// owning package (Rules 13/14): a literal here would go green while the
	// report drifted from the behaviour it describes.
	baseline := config.DefaultEffortValues()
	if len(baseline) == 0 {
		t.Fatal("config.DefaultEffortValues() returned nothing")
	}
	for _, member := range []string{"minimal", "high"} {
		if !strings.Contains(row, member) {
			t.Errorf("row %q does not name %q — a reader still cannot tell whether that effort value is legal", row, member)
		}
	}
	for _, member := range baseline {
		if !strings.Contains(wantDefault, member) || !strings.Contains(row, member) {
			t.Errorf("baseline member %q is missing from the default (%q) or from the row %q", member, wantDefault, row)
		}
	}
	if !strings.Contains(row, "["+runtimeConfigChannelDefault+"]") {
		t.Errorf("row %q is not labelled %q — it must be the same shape as the max_tokens and context_window rows", row, runtimeConfigChannelDefault)
	}
	if !strings.Contains(row, runtimeConfigChannelUnset) {
		t.Errorf("row %q does not say the process holds nothing (%q)", row, runtimeConfigChannelUnset)
	}

	// (2) THE DOCUMENT DECLARED ["low","max"]. A declared list reaches the
	// process through config.ApplyEnv, which is why the fixture carries it in the
	// environment AND records the settings channel — that is the state a real
	// settings.json load leaves behind. (A document value the process never
	// received is the NOT-APPLIED channel, which has its own guard above; it is a
	// different fact and is not what this claim is about.)
	declared := "low,max"
	deps := runtimeConfigTestDeps(
		map[string]string{"GOPI_EFFORT_VALUES": declared},
		map[string]string{settingsKeyEffortValues: declared},
		runtimeConfigEnvSources{Settings: map[string]bool{"GOPI_EFFORT_VALUES": true}},
	)
	declaredRow := rowFor(renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil, deps)), settingsKeyEffortValues)
	if declaredRow == "" {
		t.Fatalf("no %q row with a declared list", settingsKeyEffortValues)
	}
	if !strings.Contains(declaredRow, declared) {
		t.Errorf("row %q does not print the list the document declared (%q)", declaredRow, declared)
	}
	if !strings.Contains(declaredRow, "["+runtimeConfigChannelSettings+"]") {
		t.Errorf("row %q does not name the settings.json channel", declaredRow)
	}
	if strings.Contains(declaredRow, runtimeConfigChannelDefault) {
		t.Errorf("row %q still claims a built-in default while the document declared one — the same defect pointing the other way", declaredRow)
	}
}

// TestRuntimeConfigReportsTheClampedThinkingBudget is THE regression guard for
// the 2026-11-14 defect: the table's two budget rows reported what the
// CONFIGURATION said while every request body in the same run carried the
// clamped number.
//
// THE MEASURED SHAPE BEFORE THE FIX (`/config` on a run with
// `thinking_budget: 16384` and the default output cap):
//
//	thinking_budget               16384  [settings.json]
//	agent.thinking.budget_tokens  16384  [agent]
//
// and the request body of that very run, read out of its raw log:
//
//	"thinking":{"type":"enabled","budget_tokens":8192}
//
// The builder caps the budget at max_tokens/2 (deepseek.EffectiveThinkingBudget
// — the ONE definition point, Rule 2/14), so both rows were out by a factor of
// two, and 8192 is also the token count the harness aborts a round at. A reader
// budgeting context from this table could not have known either fact.
//
// WHAT IS PINNED, in three claims a naive fix drops:
//
//  1. BOTH rows name the EFFECTIVE number (8192) and the configured one, in the
//     value cell — a cell with only 8192 hides the operator's setting, a cell
//     with only 16384 is the defect itself.
//  2. The `!` note states the clamp, the arithmetic and the reason, and it is
//     stated ONCE even though two rows carry the same measurement (two sentences
//     about one fact read as two facts).
//  3. A budget under the cap is NOT reported as clamped — a report that cries
//     wolf on every run has no value in the one column that must never lie.
func TestRuntimeConfigReportsTheClampedThinkingBudget(t *testing.T) {
	const maxTokens = deepseek.DefaultClaudeMaxTokens // 16384
	configured := maxTokens                           // the measured operator setting
	wantEffective := deepseek.EffectiveThinkingBudget(maxTokens, configured)
	if wantEffective >= configured {
		t.Fatalf("premise gone: EffectiveThinkingBudget(%d, %d) = %d does not clamp — this test would assert nothing",
			maxTokens, configured, wantEffective)
	}

	// A fixture agent carrying EXACTLY what the resolver would carry: thinking
	// enabled with the configured budget, and the default output cap.
	ag := &agent.Agent{
		Model:         "fixture-model",
		Thinking:      &deepseek.Thinking{Type: thinkmode.Enabled, BudgetTokens: configured},
		MaxTokens:     maxTokens,
		ContextWindow: agent.DefaultContextWindow,
	}
	env := map[string]string{settingsEnvThinkingBudget: strconv.Itoa(configured)}
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag,
		runtimeConfigTestDeps(env, map[string]string{settingsKeyThinkingBudget: strconv.Itoa(configured)},
			runtimeConfigEnvSources{Settings: map[string]bool{settingsEnvThinkingBudget: true}})))
	body := strings.Join(lines, "\n")

	// (1) Both budget rows carry the effective number AND the configured one.
	for _, key := range runtimeConfigBudgetKeys {
		row := rowFor(lines, key)
		if row == "" {
			t.Fatalf("no %q row:\n%s", key, body)
		}
		want := fmt.Sprintf(runtimeConfigClampedValueFormat, wantEffective, configured)
		if !strings.Contains(row, want) {
			t.Errorf("row %q does not carry %q — the value column still reports the configured number as if it reached the wire:\n%s",
				key, want, body)
		}
		if !strings.Contains(row, strconv.Itoa(wantEffective)) {
			t.Errorf("row %q does not name the number the request will carry (%d): %q", key, wantEffective, row)
		}
	}

	// (2) The clamp is stated in words, with its arithmetic, exactly once.
	wantNote := runtimeConfigNoteMarker + settingsKeyThinkingBudget + ": thinking budget is clamped"
	if !strings.Contains(body, wantNote) {
		t.Fatalf("the clamp is not stated as a %q note:\n%s", strings.TrimSpace(runtimeConfigNoteMarker), body)
	}
	if !strings.Contains(body, "clamped") || !strings.Contains(body, strconv.Itoa(maxTokens)+"/2") {
		t.Errorf("the clamp note does not state WHY (%d/2):\n%s", maxTokens, body)
	}
	if got := strings.Count(body, "thinking budget is clamped"); got != 1 {
		t.Errorf("the clamp is stated %d time(s) — two rows carrying one measurement must be one note:\n%s", got, body)
	}
}

// TestRuntimeConfigReportsNoClampUnderTheCap is the CONTROL: a budget at or
// below max_tokens/2 reaches the wire unchanged, so the rows must print the
// plain number and the table must carry no clamp note at all. Without this arm a
// "fix" that printed the clamp sentence unconditionally would pass the guard
// above while telling every reader their harmless setting had been lowered.
func TestRuntimeConfigReportsNoClampUnderTheCap(t *testing.T) {
	const maxTokens = deepseek.DefaultClaudeMaxTokens
	configured := deepseek.DefaultClaudeThinkingBudget // 1024, far below the cap
	if got := deepseek.EffectiveThinkingBudget(maxTokens, configured); got != configured {
		t.Fatalf("premise gone: EffectiveThinkingBudget(%d, %d) = %d — the code's own default is no longer under the cap",
			maxTokens, configured, got)
	}

	ag := &agent.Agent{
		Model:         "fixture-model",
		Thinking:      &deepseek.Thinking{Type: thinkmode.Enabled, BudgetTokens: configured},
		MaxTokens:     maxTokens,
		ContextWindow: agent.DefaultContextWindow,
	}
	env := map[string]string{settingsEnvThinkingBudget: strconv.Itoa(configured)}
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag,
		runtimeConfigTestDeps(env, map[string]string{}, runtimeConfigEnvSources{})))
	body := strings.Join(lines, "\n")

	if strings.Contains(body, "clamped") {
		t.Fatalf("a budget under the cap was reported as clamped:\n%s", body)
	}
	for _, key := range runtimeConfigBudgetKeys {
		row := rowFor(lines, key)
		if row == "" {
			t.Fatalf("no %q row:\n%s", key, body)
		}
		if !strings.Contains(row, strconv.Itoa(configured)) {
			t.Errorf("row %q does not carry the budget in force (%d): %q", key, configured, row)
		}
		if strings.Contains(row, "configured") {
			t.Errorf("row %q carries clamped wording for an unclamped number: %q", key, row)
		}
	}
}

// TestRuntimeConfigClampNeedsAThinkingRequestAndAnAgent pins the TWO premises
// the clamp row must not jump over, each asserted from the render rather than
// from the predicate, because the defect would appear as a row, not as a bool:
//
//   - thinking DISABLED: the builder never sets BudgetTokens at all, so the
//     budget does not reach the wire and naming a clamped number would be a
//     third false answer;
//   - NO AGENT (the web table's carrier, `slashConfigCommand` with a nil agent):
//     max_tokens and the resolved budget are the agent's own fields, so the
//     denominator would have to be re-resolved from the environment — the
//     second resolution Rule 2 forbids.
func TestRuntimeConfigClampNeedsAThinkingRequestAndAnAgent(t *testing.T) {
	const maxTokens = deepseek.DefaultClaudeMaxTokens
	configured := maxTokens
	env := map[string]string{settingsEnvThinkingBudget: strconv.Itoa(configured)}

	disabled := &agent.Agent{
		Model:         "fixture-model",
		Thinking:      &deepseek.Thinking{Type: thinkmode.Disabled, BudgetTokens: configured},
		MaxTokens:     maxTokens,
		ContextWindow: agent.DefaultContextWindow,
	}
	for _, c := range []struct {
		name string
		ag   *agent.Agent
	}{
		{"thinking disabled — the budget never reaches the wire", disabled},
		{"no agent — nothing resolved the denominator", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(c.ag,
				runtimeConfigTestDeps(env, map[string]string{}, runtimeConfigEnvSources{})))
			body := strings.Join(lines, "\n")
			if strings.Contains(body, "clamped") {
				t.Fatalf("%s, yet the report claims a clamp:\n%s", c.name, body)
			}
			if row := rowFor(lines, settingsKeyThinkingBudget); strings.Contains(row, "configured") {
				t.Errorf("the %s row carries clamped wording: %q", settingsKeyThinkingBudget, row)
			}
		})
	}
}
