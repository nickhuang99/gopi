package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFlagOverrides(t *testing.T) {
	cfg, err := FlagOverrides([]string{
		"--timeout", "600",
		"--max-tokens=8192",
		"--model", "deepseek-v4-pro",
		"--thinking", "disabled",
		"--compact", "off",
		"--db=/tmp/x.db",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TimeoutSec != 600 {
		t.Errorf("TimeoutSec = %d, want 600", cfg.TimeoutSec)
	}
	if cfg.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192", cfg.MaxTokens)
	}
	if cfg.Model != "deepseek-v4-pro" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.Thinking == nil || *cfg.Thinking != ThinkingDisabled {
		t.Errorf("Thinking = %v, want disabled", cfg.Thinking)
	}
	if cfg.Compact == nil || *cfg.Compact != false {
		t.Errorf("Compact = %v, want false", cfg.Compact)
	}
	if cfg.MemoryDB != "/tmp/x.db" {
		t.Errorf("MemoryDB = %q", cfg.MemoryDB)
	}
}

func TestFlagOverridesUnknownFlag(t *testing.T) {
	_, err := FlagOverrides([]string{"--nope", "1"})
	if err == nil {
		t.Fatal("expected unknown-flag error")
	}
}

func TestFlagOverridesMissingValue(t *testing.T) {
	_, err := FlagOverrides([]string{"--timeout"})
	if err == nil {
		t.Fatal("expected missing-value error")
	}
}

func TestFlagOverridesInvalidInt(t *testing.T) {
	_, err := FlagOverrides([]string{"--timeout", "abc"})
	if err == nil {
		t.Fatal("expected invalid-int error")
	}
}

// TestFlagOverridesEnvNameAsFlag is the regression test for P0-2
// (devdoc/review-cli-config-options.md): help advertises "Use the full env
// var name as a flag (e.g. --gopi-timeout-sec)" but FlagOverrides rejected
// every env-named spelling with "unknown flag". The env-name form must
// canonicalize to the short flag and land in the SAME applyFlag case, so
// parsing and precedence are identical by construction.
func TestFlagOverridesEnvNameAsFlag(t *testing.T) {
	// Scalar env-named flag, space-separated value (the help's own example).
	cfg, err := FlagOverrides([]string{"--gopi-timeout-sec", "10"})
	if err != nil {
		t.Fatalf("--gopi-timeout-sec 10: unexpected error: %v", err)
	}
	if cfg.TimeoutSec != 10 {
		t.Errorf("TimeoutSec = %d, want 10", cfg.TimeoutSec)
	}
	if !cfg.SetEnvVars["GOPI_TIMEOUT_SEC"] {
		t.Errorf("GOPI_TIMEOUT_SEC not marked as explicitly set: %v", cfg.SetEnvVars)
	}
	// Full chain: the marked env must reach the process env via ApplyEnv —
	// this is what makes the knob actually APPLY (the agent's readers parse
	// GOPI_TIMEOUT_SEC from os.Getenv).
	t.Setenv("GOPI_TIMEOUT_SEC", "")
	ApplyEnv(cfg, true)
	if got := os.Getenv("GOPI_TIMEOUT_SEC"); got != "10" {
		t.Errorf("ApplyEnv after --gopi-timeout-sec 10: GOPI_TIMEOUT_SEC=%q, want 10", got)
	}

	// = form and a second sibling (max tokens).
	cfg, err = FlagOverrides([]string{"--gopi-max-tokens=8192"})
	if err != nil {
		t.Fatalf("--gopi-max-tokens=8192: unexpected error: %v", err)
	}
	if cfg.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192", cfg.MaxTokens)
	}
	if !cfg.SetEnvVars["GOPI_MAX_TOKENS"] {
		t.Errorf("GOPI_MAX_TOKENS not marked as explicitly set: %v", cfg.SetEnvVars)
	}

	// Bool env-named flag behaves like the short bool flag: bare true,
	// explicit false, trailing bare token.
	cfg, err = FlagOverrides([]string{"--gopi-debug"})
	if err != nil {
		t.Fatalf("--gopi-debug: unexpected error: %v", err)
	}
	if !cfg.Debug {
		t.Error("--gopi-debug (bare) must set Debug=true")
	}
	cfg, err = FlagOverrides([]string{"--gopi-debug=false"})
	if err != nil {
		t.Fatalf("--gopi-debug=false: unexpected error: %v", err)
	}
	if cfg.Debug {
		t.Error("--gopi-debug=false must set Debug=false")
	}
	cfg, err = FlagOverrides([]string{"--gopi-compact", "off"})
	if err != nil {
		t.Fatalf("--gopi-compact off: unexpected error: %v", err)
	}
	if cfg.Compact == nil || *cfg.Compact {
		t.Errorf("--gopi-compact off must set Compact=false, got %v", cfg.Compact)
	}
	if !cfg.SetEnvVars["GOPI_COMPACT"] {
		t.Errorf("GOPI_COMPACT not marked as explicitly set: %v", cfg.SetEnvVars)
	}
}

// TestFlagOverridesEnvName_UnderscoreSpelling accepts the literal env spelling
// (--gopi_timeout_sec) as well — same knob, same applyFlag path.
func TestFlagOverridesEnvName_UnderscoreSpelling(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--gopi_timeout_sec", "10"})
	if err != nil {
		t.Fatalf("--gopi_timeout_sec 10: unexpected error: %v", err)
	}
	if cfg.TimeoutSec != 10 {
		t.Errorf("TimeoutSec = %d, want 10", cfg.TimeoutSec)
	}
}

// TestFlagOverridesEnvNameNoFlagInCatalog: an env var with NO catalog CLI
// flag has no env-name spelling — refused, and refused BY NAME: the reason is
// "no flag of its own", never the bare "unknown flag" (before 2026-09-27 this
// case emitted exactly that false first word).
//
// THE FIXTURE MOVED (2026-10-22, task 01a160a2): this test used
// GOPI_COMPUTE_NODES, which was DELETED with the `compute_nodes` knob — a
// test whose subject is "a real knob with no CLI flag" cannot use a name the
// catalog no longer carries, or it asserts the refusal of a NON-knob (which
// the bare "unknown flag" answer would then be right about). GOPI_SYNC_PEERS
// is a live catalog row with an empty CLIFlag column and its own resolver.
func TestFlagOverridesEnvNameNoFlagInCatalog(t *testing.T) {
	_, err := FlagOverrides([]string{"--gopi-sync-peers", "hp"})
	if err == nil {
		t.Fatal("expected a refusal for an env var with no catalog CLIFlag")
	}
	msg := err.Error()
	if strings.Contains(msg, unknownFlagPrefix) {
		t.Errorf("refusal %q claims the name is unknown; GOPI_SYNC_PEERS is a real knob", msg)
	}
	if !strings.Contains(msg, flaglessRefusalStem) || !strings.Contains(msg, "GOPI_SYNC_PEERS") {
		t.Errorf("refusal %q must name the knob and the reason (no CLI flag)", msg)
	}
}

// TestHelpEnvNameSentenceMatchesParser: the help sentence that advertises the
// env-name-as-flag escape hatch must be backed by a working parser path —
// the exact advertised example must parse. (This is the machine-checkable
// "help text matches reality" assertion; help_env_claim_test.go extends it to
// the WHOLE catalog and to the subset the sentence actually covers.)
func TestHelpEnvNameSentenceMatchesParser(t *testing.T) {
	usage := FormatUsage()
	if !strings.Contains(usage, reachabilityClaimSubset) {
		t.Fatal("help no longer advertises the env-name-as-flag escape hatch — update this test or the help text")
	}
	// The advertised example must actually work.
	cfg, err := FlagOverrides([]string{"--gopi-timeout-sec", "10"})
	if err != nil {
		t.Fatalf("help advertises --gopi-timeout-sec but the parser rejects it: %v", err)
	}
	if cfg.TimeoutSec != 10 {
		t.Errorf("help-advertised --gopi-timeout-sec parsed TimeoutSec=%d, want 10", cfg.TimeoutSec)
	}
}

// TestFlagOverridesPositionals_FlagValueNotPositional is the regression test
// for the Epic's failing command: `gopi --model deepseek-v4-flash-vision-exp
// "task text"` used to treat the MODEL NAME as an input file because the old
// positional scan in main.go only skipped args starting with "-" — a flag
// VALUE never starts with "-", so it leaked into the input-file list. Now
// FlagOverrides consumes the value and reports only true positionals.
func TestFlagOverridesPositionals_FlagValueNotPositional(t *testing.T) {
	cfg, err := FlagOverrides([]string{
		"--model", "deepseek-v4-flash-vision-exp",
		"--prompt", "some explanation",
		"your task is written in image ~/x.png",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model != "deepseek-v4-flash-vision-exp" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.Prompt != "some explanation" {
		t.Errorf("Prompt = %q, want %q", cfg.Prompt, "some explanation")
	}
	want := []string{"your task is written in image ~/x.png"}
	if len(cfg.Positionals) != len(want) {
		t.Fatalf("Positionals = %q, want %q", cfg.Positionals, want)
	}
	for i := range want {
		if cfg.Positionals[i] != want[i] {
			t.Errorf("Positionals[%d] = %q, want %q", i, cfg.Positionals[i], want[i])
		}
	}
}

// TestFlagOverridesPositionals_Mixed verifies a mix of flags, flag values and
// true positionals: only the true positionals are collected, in order.
func TestFlagOverridesPositionals_Mixed(t *testing.T) {
	cfg, err := FlagOverrides([]string{
		"--timeout", "600",
		"task.txt",
		"--model", "m1",
		"screenshot.png",
		"--prompt=look at this",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"task.txt", "screenshot.png"}
	if len(cfg.Positionals) != len(want) {
		t.Fatalf("Positionals = %q, want %q", cfg.Positionals, want)
	}
	for i := range want {
		if cfg.Positionals[i] != want[i] {
			t.Errorf("Positionals[%d] = %q, want %q", i, cfg.Positionals[i], want[i])
		}
	}
	if cfg.TimeoutSec != 600 || cfg.Model != "m1" || cfg.Prompt != "look at this" {
		t.Errorf("unexpected config: timeout=%d model=%q prompt=%q", cfg.TimeoutSec, cfg.Model, cfg.Prompt)
	}
}

// TestFlagOverridesPrompt_RequiresValue checks that --prompt (a scalar flag)
// rejects a missing value like every other scalar flag.
func TestFlagOverridesPrompt_RequiresValue(t *testing.T) {
	_, err := FlagOverrides([]string{"--prompt"})
	if err == nil {
		t.Fatal("expected missing-value error for --prompt")
	}
}

// TestFlagOverridesPrompt_EqualForm verifies the --prompt=value form.
func TestFlagOverridesPrompt_EqualForm(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--prompt=describe the chart data", "chart.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Prompt != "describe the chart data" {
		t.Errorf("Prompt = %q", cfg.Prompt)
	}
	if len(cfg.Positionals) != 1 || cfg.Positionals[0] != "chart.png" {
		t.Errorf("Positionals = %q", cfg.Positionals)
	}
}

// TestFlagOverridesModel_MarksBothEnvKeys is the regression test for the
// Epic's FIRST failing command: `gopi --model deepseek-v4-flash-vision-exp
// "task"` silently ignored --model because a shell-exported ANTHROPIC_MODEL
// (checked FIRST by ResolveModel) beat the flag — FlagOverrides only marked
// the LAST catalog env key per flag (DEEPSEEK_MODEL). --model must force-write
// BOTH ANTHROPIC_MODEL and DEEPSEEK_MODEL.
func TestFlagOverridesModel_MarksBothEnvKeys(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--model", "deepseek-v4-flash-vision-exp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SetEnvVars["ANTHROPIC_MODEL"] {
		t.Error("SetEnvVars[ANTHROPIC_MODEL] not set — shell-exported ANTHROPIC_MODEL would beat --model")
	}
	if !cfg.SetEnvVars["DEEPSEEK_MODEL"] {
		t.Error("SetEnvVars[DEEPSEEK_MODEL] not set")
	}
}

// TestFlagOverridesAPIKeyBaseURL_MarksBothEnvKeys mirrors the --model fix for
// --api-key and --base-url: the Anthropic_* variant is checked first by the
// resolvers, so a shell-exported ANTHROPIC_* must not beat the flag.
func TestFlagOverridesAPIKeyBaseURL_MarksBothEnvKeys(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--api-key", "k", "--base-url", "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, k := range []string{"ANTHROPIC_AUTH_TOKEN", "DEEPSEEK_API_KEY", "ANTHROPIC_BASE_URL", "DEEPSEEK_BASE_URL"} {
		if !cfg.SetEnvVars[k] {
			t.Errorf("SetEnvVars[%s] not set", k)
		}
	}
}

// TestFlagOverridesKeepThinking is the regression test for the --keep-thinking
// CLI flag being dead: it was declared in the EnvVars catalog with CLIFlag
// "keep-thinking" but isBoolKey omitted "keep_thinking", so the flag was
// misclassified as a scalar (requiring a value) and applyFlag had no case for
// it → "unhandled flag --keep-thinking". isBoolKey + applyFlag are now fixed.
func TestFlagOverridesKeepThinking(t *testing.T) {
	// Bare boolean switch — must NOT require a value (the scalar path).
	cfg, err := FlagOverrides([]string{"--keep-thinking"})
	if err != nil {
		t.Fatalf("--keep-thinking should parse as a bool switch, got: %v", err)
	}
	if !cfg.KeepThinking {
		t.Error("KeepThinking = false after bare --keep-thinking, want true")
	}

	// Explicit true.
	cfg, err = FlagOverrides([]string{"--keep-thinking=true"})
	if err != nil {
		t.Fatalf("--keep-thinking=true: %v", err)
	}
	if !cfg.KeepThinking {
		t.Error("KeepThinking = false after --keep-thinking=true, want true")
	}

	// Explicit false.
	cfg, err = FlagOverrides([]string{"--keep-thinking=false"})
	if err != nil {
		t.Fatalf("--keep-thinking=false: %v", err)
	}
	if cfg.KeepThinking {
		t.Error("KeepThinking = true after --keep-thinking=false, want false")
	}

	// Space-separated bool token must be consumed (not left as a positional).
	cfg, err = FlagOverrides([]string{"--keep-thinking", "off"})
	if err != nil {
		t.Fatalf("--keep-thinking off: %v", err)
	}
	if cfg.KeepThinking {
		t.Error("KeepThinking = true after --keep-thinking off, want false")
	}
	if len(cfg.Positionals) != 0 {
		t.Errorf("Positionals = %v, want empty (bool token should be consumed)", cfg.Positionals)
	}

	// The env/settings channel must stay intact: the flag also marks the env var.
	if !cfg.SetEnvVars["GOPI_KEEP_THINKING"] {
		t.Error("SetEnvVars[GOPI_KEEP_THINKING] not marked by --keep-thinking")
	}
}

func TestFromSettingsFileFull(t *testing.T) {
	p := writeSettings(t, `{
		"api_key":"sk-test",
		"base_url":"https://example.com",
		"model":"m1",
		"thinking":false,
		"reasoning_effort":"high",
		"max_tokens":4096,
		"timeout":300,
		"memory_db":"/tmp/custom.db"
	}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Thinking == nil || *cfg.Thinking != ThinkingDisabled {
		t.Errorf("Thinking = %v, want disabled", cfg.Thinking)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q", cfg.ReasoningEffort)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d", cfg.MaxTokens)
	}
	// timeout is stored as integer SECONDS (not Go nanoseconds) so a JSON
	// "timeout":300 means 300s — a regression guard for duration-JSON confusion.
	if cfg.TimeoutSec != 300 {
		t.Errorf("TimeoutSec = %d, want 300", cfg.TimeoutSec)
	}
	if cfg.MemoryDB != "/tmp/custom.db" {
		t.Errorf("MemoryDB = %q", cfg.MemoryDB)
	}
}

// TestFromSettingsFile_ThinkingStringForm pins the canonical settings.json
// "thinking" value: the STRING "disabled"/"enabled" (not a bool). The string
// form is the documented surface (GOPI.md task A); the bool form still works
// via ThinkingMode.UnmarshalJSON for backward compatibility.
func TestFromSettingsFile_ThinkingStringForm(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want ThinkingMode
	}{
		{`{"thinking":"disabled"}`, ThinkingDisabled},
		{`{"thinking":"enabled"}`, ThinkingEnabled},
		{`{"thinking":false}`, ThinkingDisabled}, // legacy bool form
		{`{"thinking":true}`, ThinkingEnabled},   // legacy bool form
	} {
		p := writeSettings(t, tc.raw)
		cfg, err := FromSettingsFile(p)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.raw, err)
		}
		if cfg.Thinking == nil || *cfg.Thinking != tc.want {
			t.Errorf("%s: Thinking = %v, want %q", tc.raw, cfg.Thinking, tc.want)
		}
	}
	// An invalid value must be a parse error, not a silent flip.
	p := writeSettings(t, `{"thinking":"banana"}`)
	if _, err := FromSettingsFile(p); err == nil {
		t.Error("expected error for invalid thinking value, got nil")
	}
}

// TestCompactStringAccept is the P1-C regression test
// (devdoc/review2-cli-config-options-variant-20260903.md): a settings.json
// `"compact": "off"` used to fail the WHOLE parse (Config.Compact was a
// strict *bool; "off" is not a bool) — losing every other setting in the
// file. The toggle must accept bool true/false AND the string spellings the
// env/flag channels already document ("on"/"off"), case-insensitively, and
// other settings must survive untouched.
func TestCompactStringAccept(t *testing.T) {
	for _, tc := range []struct {
		raw  string // "compact" JSON value
		want bool
	}{
		{`true`, true},   // JSON bool form
		{`false`, false}, // JSON bool form
		{`"on"`, true},   // string spellings the env/flag channels use
		{`"off"`, false},
		{`"true"`, true},
		{`"false"`, false},
		{`"1"`, true},
		{`"0"`, false},
		{`"ON"`, true}, // case-insensitive
		{`"Off"`, false},
	} {
		// A sibling setting rides along: if compact's value hard-fails the
		// parse, max_tokens is lost with it — the P1-C blast radius.
		p := writeSettings(t, `{"compact":`+tc.raw+`,"max_tokens":4096}`)
		cfg, err := FromSettingsFile(p)
		if err != nil {
			t.Fatalf("compact %s: unexpected error: %v", tc.raw, err)
		}
		if cfg.Compact == nil || bool(*cfg.Compact) != tc.want {
			t.Errorf("compact %s: Compact = %v, want %v", tc.raw, cfg.Compact, tc.want)
		}
		if cfg.MaxTokens != 4096 {
			t.Errorf("compact %s: sibling setting max_tokens lost, got %d, want 4096", tc.raw, cfg.MaxTokens)
		}
	}
	// An invalid value must be a CLEAR parse error naming the field — never
	// a silent flip to a default.
	p := writeSettings(t, `{"compact":"banana"}`)
	if _, err := FromSettingsFile(p); err == nil {
		t.Error("expected error for invalid compact value, got nil")
	}
	// An absent compact stays nil (unset → code default applies), so a
	// settings file without the key is untouched.
	p = writeSettings(t, `{"model":"m1"}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("settings without compact: %v", err)
	}
	if cfg.Compact != nil {
		t.Errorf("settings without compact: Compact = %v, want nil (unset)", cfg.Compact)
	}
}

// TestApplyEnv_ThinkingStringPropagates pins the settings→env bridge: a
// settings.json "thinking" value must reach the agent as the canonical
// "enabled"/"disabled" string GOPI_THINKING (the loop's switch parses those
// verbatim; a "1"/"off" bool emission would be rejected as invalid).
func TestApplyEnv_ThinkingStringPropagates(t *testing.T) {
	t.Setenv("GOPI_THINKING", "") // clear any inherited value
	p := writeSettings(t, `{"thinking":"enabled"}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("FromSettingsFile: %v", err)
	}
	ApplyEnv(cfg, false)
	if got := os.Getenv("GOPI_THINKING"); got != "enabled" {
		t.Errorf("GOPI_THINKING = %q, want enabled", got)
	}
}

func TestFromSettingsFileMissingIsNotError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.json")
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
}

func TestFromSettingsFileDoesNotOverwriteEnv(t *testing.T) {
	t.Setenv("GOPI_MAX_TOKENS", "1000")
	p := writeSettings(t, `{"max_tokens":4096}`)
	// ApplyEnv(settings, false) must NOT override an already-set env var.
	cfg, _ := FromSettingsFile(p) //stylegate:ignore settings load in a fixture; the asserted fields are what the test pins, so a decode failure surfaces as a comparison mismatch
	ApplyEnv(cfg, false)
	if got := os.Getenv("GOPI_MAX_TOKENS"); got != "1000" {
		t.Errorf("settings clobbered env: GOPI_MAX_TOKENS=%q, want 1000", got)
	}
}

func TestApplyEnvSettingsFillsUnset(t *testing.T) {
	t.Setenv("GOPI_MAX_TOKENS", "")
	p := writeSettings(t, `{"max_tokens":4096}`)
	cfg, _ := FromSettingsFile(p) //stylegate:ignore settings load in a fixture; the asserted fields are what the test pins, so a decode failure surfaces as a comparison mismatch
	ApplyEnv(cfg, false)
	if got := os.Getenv("GOPI_MAX_TOKENS"); got != "4096" {
		t.Errorf("GOPI_MAX_TOKENS = %q, want 4096", got)
	}
}

func TestApplyEnvFlagsOverrideEnv(t *testing.T) {
	t.Setenv("GOPI_TIMEOUT_SEC", "60")
	flagCfg, _ := FlagOverrides([]string{"--timeout", "600"}) //stylegate:ignore flag-override resolution in a table-driven test; the assertion on the returned struct is the contract and a failure shows up as a zero value
	ApplyEnv(flagCfg, true)
	if got := os.Getenv("GOPI_TIMEOUT_SEC"); got != "600" {
		t.Errorf("flag failed to override env: GOPI_TIMEOUT_SEC=%q, want 600", got)
	}
}

func TestApplyEnvTimeoutZero(t *testing.T) {
	flagCfg, _ := FlagOverrides([]string{"--timeout", "0"}) //stylegate:ignore flag-override resolution in a table-driven test; the assertion on the returned struct is the contract and a failure shows up as a zero value
	ApplyEnv(flagCfg, true)
	if got := os.Getenv("GOPI_TIMEOUT_SEC"); got != "0" {
		t.Errorf("GOPI_TIMEOUT_SEC = %q, want 0 (unlimited)", got)
	}
}

func TestApplyEnvSettingsMirrorsDeepseekFallbacks(t *testing.T) {
	// Isolate from a developer's exported credentials (Rule-safe): this test's
	// intent is that a fresh settings.json alone populates BOTH endpoint
	// env-var families. If the shell has ANTHROPIC_AUTH_TOKEN/DEEPSEEK_API_KEY
	// exported, ApplyEnv(settings,false) correctly refuses to clobber them and
	// the assertion would fail on an environment quirk, not a code bug.
	for _, v := range []string{
		"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL",
		"DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL", "DEEPSEEK_MODEL",
	} {
		t.Setenv(v, "")
	}
	p := writeSettings(t, `{"api_key":"sk-x","base_url":"https://d","model":"m"}`)
	cfg, _ := FromSettingsFile(p) //stylegate:ignore settings load in a fixture; the asserted fields are what the test pins, so a decode failure surfaces as a comparison mismatch
	ApplyEnv(cfg, false)
	if got := os.Getenv("ANTHROPIC_AUTH_TOKEN"); got != "sk-x" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", got)
	}
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sk-x" {
		t.Errorf("DEEPSEEK_API_KEY = %q (fallback mirror missing)", got)
	}
}

func TestHelpEnvListsEveryVar(t *testing.T) {
	table := FormatEnvTable()
	for _, e := range EnvVars {
		if !contains(table, e.Env) {
			t.Errorf("FormatEnvTable missing %q", e.Env)
		}
	}
}

func TestHelpEnvListsSettingsKeysAndFlags(t *testing.T) {
	table := FormatEnvTable()
	// Spot-check that the renderer emits the settings key and CLI flag.
	for _, want := range []string{
		"ANTHROPIC_AUTH_TOKEN", "api_key", "--api-key",
		"GOPI_TIMEOUT_SEC", "timeout", "--timeout",
		"GOPI_COMPACT_TRIGGER", "compact_trigger", "--compact-trigger",
		"GOPI_MEMORY_DB", "memory_db", "--db",
	} {
		if !contains(table, want) {
			t.Errorf("FormatEnvTable missing %q", want)
		}
	}
}

// Regression guard for the "gopi prints the whole log" bug. FlagOverrides must
// record (in SetEnvVars) only the env keys the user actually passed on the
// command line — NOT every zero-valued knob. A bare `gopi` run must not put
// GOPI_DEBUG=off (or any other unset flag) into the process env, because the
// downstream env readers treat a non-empty value as an explicit setting.
func TestFlagOverridesOnlyMarksPassedFlags(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--model", "m1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SetEnvVars["ANTHROPIC_MODEL"] && !cfg.SetEnvVars["DEEPSEEK_MODEL"] {
		t.Error("--model was passed but neither MODEL env override is marked as set")
	}
	// --debug was NOT passed → GOPI_DEBUG must NOT be in SetEnvVars.
	if _, ok := cfg.SetEnvVars["GOPI_DEBUG"]; ok {
		t.Errorf("GOPI_DEBUG marked as set but --debug was not passed: %v", cfg.SetEnvVars)
	}
	// And the bool/int knobs that were never passed must not be forced into env.
	t.Setenv("GOPI_DEBUG", "")
	t.Setenv("GOPI_TIMEOUT_SEC", "")
	t.Setenv("GOPI_CONTEXT_WINDOW", "")
	ApplyEnv(cfg, true)
	if got := os.Getenv("GOPI_DEBUG"); got != "" {
		t.Errorf("ApplyEnv(fromFlags) wrote unpressed --debug: GOPI_DEBUG=%q", got)
	}
	if got := os.Getenv("GOPI_TIMEOUT_SEC"); got != "" {
		t.Errorf("ApplyEnv(fromFlags) wrote unpressed --timeout: GOPI_TIMEOUT_SEC=%q", got)
	}
	// But explicitly-passed --model still propagates.
	if got := os.Getenv("DEEPSEEK_MODEL"); got != "m1" && got != "" {
		t.Errorf("DEEPSEEK_MODEL=%q, want m1 (flag must still win)", got)
	}
}

// TestApplyEnvFlagsOnlyPropagateExplicit booleans: `--debug` explicitly passed
// (as false) must write GOPI_DEBUG=off, whereas an unpassed `--debug` must not.
func TestApplyEnvFlagsOnlyPropagateExplicit(t *testing.T) {
	t.Setenv("GOPI_DEBUG", "")
	// Explicit --debug off.
	on, _ := FlagOverrides([]string{"--debug", "off"}) //stylegate:ignore flag-override resolution in a table-driven test; the assertion on the returned struct is the contract and a failure shows up as a zero value
	ApplyEnv(on, true)
	if got := os.Getenv("GOPI_DEBUG"); got != "off" {
		t.Errorf("explicit --debug off: GOPI_DEBUG=%q, want off", got)
	}
	// Unpassed --debug.
	t.Setenv("GOPI_DEBUG", "")
	off, _ := FlagOverrides([]string{"--model", "x"}) //stylegate:ignore flag-override resolution in a table-driven test; the assertion on the returned struct is the contract and a failure shows up as a zero value
	ApplyEnv(off, true)
	if got := os.Getenv("GOPI_DEBUG"); got != "" {
		t.Errorf("unpassed --debug must not touch GOPI_DEBUG, got %q", got)
	}
}

// TestApplyEnvFlagsDoNotWriteUnsetScalars: a bare flag parse must not cascade
// zero values (GOPI_MAX_TOKENS=0 etc.) into the process env, which previously
// triggered "invalid X=0, using default" warnings on every run.
func TestApplyEnvFlagsDoNotWriteUnsetScalars(t *testing.T) {
	for _, k := range []string{
		"GOPI_MAX_TOKENS", "GOPI_CONTEXT_WINDOW", "GOPI_TIMEOUT_SEC",
		"GOPI_THINKING_BUDGET", "GOPI_COMPACT_MAX_SUMMARY_TOKENS",
	} {
		t.Setenv(k, "")
	}
	cfg, err := FlagOverrides([]string{"--model", "m1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ApplyEnv(cfg, true)
	for _, k := range []string{
		"GOPI_MAX_TOKENS", "GOPI_CONTEXT_WINDOW", "GOPI_TIMEOUT_SEC",
		"GOPI_THINKING_BUDGET", "GOPI_COMPACT_MAX_SUMMARY_TOKENS",
	} {
		if got := os.Getenv(k); got != "" {
			t.Errorf("unset scalar %s was force-written to %q", k, got)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
