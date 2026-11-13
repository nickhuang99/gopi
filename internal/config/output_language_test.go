package config

// output_language_test.go — the reply-language knob (settings.json
// "output_language" / GOPI_OUTPUT_LANGUAGE / --output-language).
//
// The contract has two halves and both are asserted here, because a VALUE knob
// fails in two opposite ways: it can emit something when nobody configured it
// (the compatibility contract — an unset knob must leave the prompt
// byte-identical), or it can silently swallow a value the operator DID write
// (the Rule 15 defect: "I did not understand you" and "nobody asked" are the
// same empty string and two different facts).
//
// Rule 17: every env flip is t.Setenv (test-scoped).

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOutputLanguageSettings writes a settings.json fixture and points
// GOPI_SETTINGS at it, with the env channel cleared.
func writeOutputLanguageSettings(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsEnv, p)
	t.Setenv(OutputLanguageEnv, "")
}

// captureStderr runs fn with os.Stderr redirected and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		done <- string(b)
	}()
	fn()
	_ = w.Close() //stylegate:ignore closes the fixture write end
	os.Stderr = old
	return <-done
}

// TestOutputLanguageUnsetEmitsNothing is the compatibility contract's first
// half: with neither channel set, the resolution is "" AND the directive is ""
// — the caller (internal/agent) prepends the empty string, which is what makes
// the assembled prompt byte-identical to the pre-knob one.
func TestOutputLanguageUnsetEmitsNothing(t *testing.T) {
	writeOutputLanguageSettings(t, `{"model":"m"}`)
	if got := OutputLanguage(); got != "" {
		t.Fatalf("OutputLanguage() = %q, want \"\" when neither channel declares it", got)
	}
	if got := OutputLanguageDirective(); got != "" {
		t.Fatalf("OutputLanguageDirective() = %q, want \"\" — an unset knob must emit NOTHING", got)
	}
}

// TestOutputLanguageEmptySettingsValueIsUnset pins the shape an operator
// actually writes when he wants to DISABLE a knob he set earlier:
// `"output_language": ""`. It must be silent, not a directive naming no
// language.
func TestOutputLanguageEmptySettingsValueIsUnset(t *testing.T) {
	writeOutputLanguageSettings(t, `{"output_language":""}`)
	if got := OutputLanguage(); got != "" {
		t.Fatalf("an empty settings value must resolve to unset, got %q", got)
	}
	if got := OutputLanguageDirective(); got != "" {
		t.Fatalf("an empty settings value must emit no directive, got %q", got)
	}
}

// TestOutputLanguageSettingsChannelAlone drives the settings-file face.
func TestOutputLanguageSettingsChannelAlone(t *testing.T) {
	writeOutputLanguageSettings(t, `{"output_language":"zh-CN"}`)
	if got := OutputLanguage(); got != "zh-CN" {
		t.Fatalf("OutputLanguage() = %q, want zh-CN from the settings file", got)
	}
	d := OutputLanguageDirective()
	if !strings.Contains(d, "zh-CN") {
		t.Fatalf("the directive must name the configured tag, got %q", d)
	}
}

// TestOutputLanguageEnvChannelAlone drives the env face, and asserts the
// directive is a DIRECTIVE (it states the rule and the English-body override),
// not a bare tag that a model could read as a topic.
func TestOutputLanguageEnvChannelAlone(t *testing.T) {
	writeOutputLanguageSettings(t, `{}`)
	t.Setenv(OutputLanguageEnv, "zh-CN")
	if got := OutputLanguage(); got != "zh-CN" {
		t.Fatalf("OutputLanguage() = %q, want zh-CN from %s", got, OutputLanguageEnv)
	}
	d := OutputLanguageDirective()
	if !strings.HasPrefix(d, "[OUTPUT LANGUAGE") {
		t.Fatalf("the directive must open with its own rule label, got %q", d)
	}
	if !strings.Contains(d, "overrides") {
		t.Fatalf("the directive must state that it overrides the English body, got %q", d)
	}
}

// TestOutputLanguagePrecedenceEnvBeatsSettings pins rung 2 over rung 3.
func TestOutputLanguagePrecedenceEnvBeatsSettings(t *testing.T) {
	writeOutputLanguageSettings(t, `{"output_language":"zh-CN"}`)
	t.Setenv(OutputLanguageEnv, "en-GB")
	if got := OutputLanguage(); got != "en-GB" {
		t.Fatalf("env must beat settings: got %q, want en-GB", got)
	}
}

// TestOutputLanguageFlagBeatsEnv pins rung 1 over rung 2 through the REAL
// mechanism: BootstrapEnvFromArgs hoists the flag into the knob's env name
// unconditionally, which is what makes `gopi --output-language X chat` win over
// an exported variable (the chat/group-verb paths return from main() before the
// agent-path ApplyEnv, so the hoist is the only thing that reaches them).
func TestOutputLanguageFlagBeatsEnv(t *testing.T) {
	writeOutputLanguageSettings(t, `{"output_language":"zh-CN"}`)
	t.Setenv(OutputLanguageEnv, "ja")
	rest := BootstrapEnvFromArgs([]string{"--output-language", "en-GB", "chat"})
	if got := os.Getenv(OutputLanguageEnv); got != "en-GB" {
		t.Fatalf("the flag must overwrite the env knob: env=%q, want en-GB", got)
	}
	if got := OutputLanguage(); got != "en-GB" {
		t.Fatalf("resolution after the flag hoist = %q, want en-GB (flag > env > settings)", got)
	}
	if len(rest) != 1 || rest[0] != "chat" {
		t.Fatalf("the hoisted flag pair must be consumed: rest=%v, want [chat]", rest)
	}
}

// TestOutputLanguageFlagEqualsForm checks the second CLI spelling the parser
// documents (`--output-language=zh-CN`).
func TestOutputLanguageFlagEqualsForm(t *testing.T) {
	writeOutputLanguageSettings(t, `{}`)
	t.Setenv(OutputLanguageEnv, "")
	BootstrapEnvFromArgs([]string{"--output-language=zh-CN", "chat"})
	if got := OutputLanguage(); got != "zh-CN" {
		t.Fatalf("--output-language=TAG must hoist: got %q", got)
	}
}

// TestOutputLanguageFlagParsedIntoConfig pins the third face of the same knob:
// FlagOverrides binds it onto the Config (so `--help-env` / the catalog claims
// are truthful) independent of the env hoist.
func TestOutputLanguageFlagParsedIntoConfig(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--output-language", "zh-CN"})
	if err != nil {
		t.Fatalf("FlagOverrides: %v", err)
	}
	if cfg.OutputLanguage != "zh-CN" {
		t.Fatalf("cfg.OutputLanguage = %q, want zh-CN", cfg.OutputLanguage)
	}
	if !cfg.SetEnvVars[OutputLanguageEnv] {
		t.Fatalf("SetEnvVars[%s] must be recorded so ApplyEnv(fromFlags) publishes it", OutputLanguageEnv)
	}
}

// TestOutputLanguageApplyEnvPublishesSettings drives the AGENT-path face: the
// settings-file value must reach the one resolver through ApplyEnv. This is the
// path `gopi "prompt"` (a bare run) takes, which never goes through
// BootstrapEnvFromArgs.
func TestOutputLanguageApplyEnvPublishesSettings(t *testing.T) {
	t.Setenv(OutputLanguageEnv, "")
	cfg := &Config{OutputLanguage: "zh-CN", SetEnvVars: map[string]bool{}}
	ApplyEnv(cfg, false)
	if got := OutputLanguage(); got != "zh-CN" {
		t.Fatalf("ApplyEnv(settings) must publish %s: resolution = %q, want zh-CN", OutputLanguageEnv, got)
	}
}

// TestOutputLanguageSettingsNeverClobbersExport is the fill-only half of
// ApplyEnv: a settings value must never overwrite a variable the caller (or the
// flag hoist) already set — otherwise the settings file would silently outrank
// both channels above it.
func TestOutputLanguageSettingsNeverClobbersExport(t *testing.T) {
	t.Setenv(OutputLanguageEnv, "en-GB")
	cfg := &Config{OutputLanguage: "zh-CN", SetEnvVars: map[string]bool{}}
	ApplyEnv(cfg, false)
	if got := OutputLanguage(); got != "en-GB" {
		t.Fatalf("settings must not clobber an explicit env value: got %q, want en-GB", got)
	}
}

// TestOutputLanguageUnrecognisedValueIsReportedAndNextChannelDecides is the
// Rule 15 acceptance: a value that is not a language tag must never be read as
// one of the valid states. It is REPORTED on stderr, and the next channel
// decides — here the settings file, which is what "the next channel" means for
// the env rung.
func TestOutputLanguageUnrecognisedValueIsReportedAndNextChannelDecides(t *testing.T) {
	writeOutputLanguageSettings(t, `{"output_language":"zh-CN"}`)
	t.Setenv(OutputLanguageEnv, "chinese (simplified)")
	var got string
	stderr := captureStderr(t, func() { got = OutputLanguage() })
	if got != "zh-CN" {
		t.Fatalf("an unusable env value must fall through to settings: got %q, want zh-CN", got)
	}
	if !strings.Contains(stderr, OutputLanguageEnv) || !strings.Contains(stderr, "chinese (simplified)") {
		t.Fatalf("the refusal must name the variable and the value it refused, got stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "not a language tag") {
		t.Fatalf("the refusal must say WHY the value was refused, got stderr=%q", stderr)
	}
}

// TestOutputLanguageUnrecognisedSettingsValueIsSilentForThePrompt is the other
// half of the same discipline: an unusable SETTINGS value (the last channel)
// leaves the knob unset — no directive, and the operator still gets the report
// rather than a silent English reply.
func TestOutputLanguageUnrecognisedSettingsValueIsSilentForThePrompt(t *testing.T) {
	writeOutputLanguageSettings(t, `{"output_language":"??"}`)
	var got string
	var directive string
	stderr := captureStderr(t, func() {
		got = OutputLanguage()
		directive = OutputLanguageDirective()
	})
	if got != "" || directive != "" {
		t.Fatalf("an unusable settings value must leave the knob unset: lang=%q directive=%q", got, directive)
	}
	if !strings.Contains(stderr, OutputLanguageSettingsKey) || !strings.Contains(stderr, `"??"`) {
		t.Fatalf("the settings refusal must name the key and the value, got stderr=%q", stderr)
	}
}

// TestOutputLanguageTagShape pins the accepted VALUE shape (BCP-47-ish), the
// single predicate the resolution uses. A shape check — never a language
// whitelist (a whitelist would refuse a tag the model understands perfectly).
func TestOutputLanguageTagShape(t *testing.T) {
	valid := []string{"zh", "en", "zh-CN", "en-GB", "pt-BR", "zh-Hans", "zh-Hans-CN", "cmn"}
	invalid := []string{"", " ", "chinese", "zh_CN", "z", "zh CN", "中文", "..", "-zh"}
	for _, v := range valid {
		if !isOutputLanguageTag(v) {
			t.Errorf("%q must be accepted as a language tag", v)
		}
	}
	for _, v := range invalid {
		if isOutputLanguageTag(v) {
			t.Errorf("%q must be REFUSED as a language tag", v)
		}
	}
}

// TestOutputLanguageCatalogRowExists pins the help/doc face: the knob must be a
// catalog row (that is what renders `gopi --help-env` and what the CONFIG.md
// gate checks), with all three channels named.
func TestOutputLanguageCatalogRowExists(t *testing.T) {
	for _, e := range EnvVars {
		if e.Env != OutputLanguageEnv {
			continue
		}
		if e.Key != OutputLanguageSettingsKey {
			t.Errorf("catalog settings key = %q, want %q", e.Key, OutputLanguageSettingsKey)
		}
		if e.CLIFlag != OutputLanguageFlagName {
			t.Errorf("catalog CLI flag = %q, want %q", e.CLIFlag, OutputLanguageFlagName)
		}
		if strings.TrimSpace(e.Help) == "" {
			t.Error("catalog help text is empty — the knob would be invisible in --help-env")
		}
		return
	}
	t.Fatalf("no catalog row for %s — the knob is undocumented and --help-env lies by omission", OutputLanguageEnv)
}

// TestOutputLanguageUnsetAndRefusedAreDistinctStates is the two-states
// contract, at the UNIT level — the defect's second half.
//
// `OutputLanguage() == ""` says only that no directive is emitted. It does not
// say WHY, and the why is two different facts with two different remedies:
// nobody asked (correct, nothing to fix) versus a declaration was DISCARDED
// (the operator configured the knob and this process did not understand him —
// he must be told). The resolution's own comment declares that contract, and
// the pre-fix code broke it: EVERY empty outcome returned source
// "not declared", so a refusal reported itself as silence.
//
// The two states are asserted through the SAME instrument an operator has —
// the resolution's reported source and the text on stderr — never by reading
// the constants back to themselves.
func TestOutputLanguageUnsetAndRefusedAreDistinctStates(t *testing.T) {
	t.Run("nobody asked", func(t *testing.T) {
		writeOutputLanguageSettings(t, `{}`)
		var lang, source string
		var stderr string
		stderr = captureStderr(t, func() { lang, source = resolveOutputLanguage() })
		if lang != "" {
			t.Fatalf("lang = %q, want \"\" — no channel declared a value", lang)
		}
		if source != outputLanguageUnset {
			t.Fatalf("source = %q, want %q — nobody declared anything, so nothing was refused",
				source, outputLanguageUnset)
		}
		if stderr != "" {
			t.Fatalf("a knob nobody configured must say NOTHING on stderr, got %q", stderr)
		}
	})

	t.Run("env refusal with no later channel", func(t *testing.T) {
		writeOutputLanguageSettings(t, `{}`)
		t.Setenv(OutputLanguageEnv, "chinese")
		var lang, source string
		stderr := captureStderr(t, func() { lang, source = resolveOutputLanguage() })
		if lang != "" {
			t.Fatalf("lang = %q, want \"\" — the refused value was not adopted", lang)
		}
		if source != outputLanguageRefused {
			t.Fatalf("source = %q, want %q — a DISCARDED declaration must not report itself "+
				"as %q (that is the collapse this test exists to prevent)",
				source, outputLanguageRefused, outputLanguageUnset)
		}
		if !strings.Contains(stderr, OutputLanguageNotAdoptedClaim) {
			t.Fatalf("the end-of-chain report must say the declaration was not adopted, got %q", stderr)
		}
		if !strings.Contains(stderr, OutputLanguageEnv) || !strings.Contains(stderr, "chinese") {
			t.Fatalf("the report must name the channel and the value it refused, got %q", stderr)
		}
	})

	t.Run("env refusal, later channel adopted", func(t *testing.T) {
		writeOutputLanguageSettings(t, `{"output_language":"zh-CN"}`)
		t.Setenv(OutputLanguageEnv, "chinese")
		var lang, source string
		stderr := captureStderr(t, func() { lang, source = resolveOutputLanguage() })
		if lang != "zh-CN" || source != OutputLanguageSettingsKey {
			t.Fatalf("a refused env value must fall through to settings: lang=%q source=%q", lang, source)
		}
		if !strings.Contains(stderr, outputLanguageSettingsKeySpelled("zh-CN")) {
			t.Fatalf("the report must name the value the chain ADOPTED instead, got %q", stderr)
		}
	})

	t.Run("settings refusal is the last channel", func(t *testing.T) {
		writeOutputLanguageSettings(t, `{"output_language":"??"}`)
		var lang, source string
		stderr := captureStderr(t, func() { lang, source = resolveOutputLanguage() })
		if lang != "" {
			t.Fatalf("lang = %q, want \"\" — the settings value was refused", lang)
		}
		if source != outputLanguageRefused {
			t.Fatalf("source = %q, want %q — a refused SETTINGS value is a discarded declaration "+
				"too (the last channel, no fallthrough), not %q",
				source, outputLanguageRefused, outputLanguageUnset)
		}
		if !strings.Contains(stderr, OutputLanguageNotAdoptedClaim) ||
			!strings.Contains(stderr, OutputLanguageSettingsKey) {
			t.Fatalf("the last-channel refusal must name the key and the chain's verdict, got %q", stderr)
		}
	})
}

// TestOutputLanguageReportIsTheValidationEntryPoint pins the ONE entry point
// the non-prompt surfaces call (cmd/agent/main.go calls it before the
// subcommand chain).
//
// WHY IT NEEDS ITS OWN TEST: the entry point's whole value is that calling it
// RUNS the validation — a function that returned early, or that called
// something other than the ONE resolution, would leave the surfaces silent
// again while every other test stayed green (they exercise the resolution
// directly). So the assertion is behavioural: after the call, a refused
// declaration must have been reported, and an unset knob must not have been.
func TestOutputLanguageReportIsTheValidationEntryPoint(t *testing.T) {
	t.Run("refused declaration is reported", func(t *testing.T) {
		writeOutputLanguageSettings(t, `{}`)
		t.Setenv(OutputLanguageEnv, "chinese")
		stderr := captureStderr(t, func() { OutputLanguageReport() })
		if !strings.Contains(stderr, OutputLanguageNotAdoptedClaim) {
			t.Fatalf("OutputLanguageReport() must run the validation and report a refused "+
				"declaration, got %q", stderr)
		}
	})

	t.Run("unset knob stays silent", func(t *testing.T) {
		writeOutputLanguageSettings(t, `{}`)
		stderr := captureStderr(t, func() { OutputLanguageReport() })
		if stderr != "" {
			t.Fatalf("OutputLanguageReport() must emit NOTHING when nobody configured the knob "+
				"(the compatibility contract), got %q", stderr)
		}
	})
}
